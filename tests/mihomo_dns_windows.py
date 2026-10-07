"""Verify real GUI DNS hijacking through TCP-only Mihomo and UDP control paths."""
import argparse
from contextlib import closing
import json
from pathlib import Path
import select
import shutil
import socket
import socketserver
import sqlite3
import struct
import tempfile
import threading
import time
from mihomo_subscription_windows import snapshot, start, stop, wait_for


def exact(sock, size):
    result = b""
    while len(result) < size:
        part = sock.recv(size - len(result))
        if not part:
            raise EOFError()
        result += part
    return result


def reply(query):
    end = 12
    while query[end]:
        end += query[end] + 1
    question = query[12:end + 5]
    return query[:2] + struct.pack("!HHHHH", 0x8180, 1, 1, 0, 0) + question + b"\xc0\x0c" + struct.pack("!HHIH", 1, 1, 60, 4) + socket.inet_aton("192.0.2.123")


class TCPDNS(socketserver.BaseRequestHandler):
    def handle(self):
        try:
            while True:
                length = struct.unpack("!H", exact(self.request, 2))[0]
                answer = reply(exact(self.request, length))
                self.server.calls += 1
                self.request.sendall(struct.pack("!H", len(answer)) + answer)
        except (EOFError, OSError):
            pass


class UDPDNS(socketserver.BaseRequestHandler):
    def handle(self):
        data, sock = self.request
        self.server.calls += 1
        sock.sendto(reply(data), self.client_address)


class SocksUDP(socketserver.BaseRequestHandler):
    def handle(self):
        data, sock = self.request
        if len(data) < 10 or data[:4] != b"\x00\x00\x00\x01":
            return
        host = socket.inet_ntoa(data[4:8])
        port = struct.unpack("!H", data[8:10])[0]
        if host != "127.0.0.1" or port != self.server.allowed_port:
            return
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as target:
            target.settimeout(5)
            target.sendto(data[10:], (host, port))
            try:
                answer = target.recv(65536)
            except OSError:
                return
        sock.sendto(data[:10] + answer, self.client_address)


class Socks(socketserver.BaseRequestHandler):
    def handle(self):
        try:
            version, count = exact(self.request, 2)
            assert version == 5
            exact(self.request, count)
            self.request.sendall(b"\x05\x00")
            version, command, _, family = exact(self.request, 4)
            assert command in (1, 3) and family == 1
            host = socket.inet_ntoa(exact(self.request, 4))
            port = struct.unpack("!H", exact(self.request, 2))[0]
            if command == 3:
                with UDPServer(("127.0.0.1", 0), SocksUDP) as relay:
                    relay.allowed_port = self.server.allowed_port
                    threading.Thread(target=relay.serve_forever, daemon=True).start()
                    try:
                        self.request.sendall(b"\x05\x00\x00\x01\x7f\x00\x00\x01" + struct.pack("!H", relay.server_address[1]))
                        while self.request.recv(1):
                            pass
                    finally:
                        relay.shutdown()
                return
            assert host == "127.0.0.1" and port == self.server.allowed_port
            with socket.create_connection((host, port), timeout=5) as target:
                self.request.sendall(b"\x05\x00\x00\x01\x7f\x00\x00\x01\x00\x00")
                while True:
                    readable, _, _ = select.select([self.request, target], [], [], 5)
                    if not readable:
                        return
                    for source in readable:
                        data = source.recv(65536)
                        if not data:
                            return
                        (target if source is self.request else self.request).sendall(data)
        except (EOFError, OSError, AssertionError):
            pass


class TCPServer(socketserver.ThreadingTCPServer):
    daemon_threads = True
    allow_reuse_address = True


class UDPServer(socketserver.ThreadingUDPServer):
    daemon_threads = True


def port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def check(exe, root, seed, label, outbound, upstream, tcp, udp, expect_tcp):
    appdata = root / label
    config = appdata / "config"
    config.mkdir(parents=True)
    db = config / "throne.db"
    snapshot(seed, db)
    inbound_port = port()
    with closing(sqlite3.connect(db)) as c:
        gid = c.execute("select id from groups order by id limit 1").fetchone()[0]
        profile_id = 999
        profile = {"type": "custom", "subtype": "outbound", "name": label, "config": json.dumps(outbound)}
        c.execute("insert into profiles(id,type,name,gid,outbound_json) values (?,?,?,?,?)", (profile_id, "custom", label, gid, json.dumps(profile)))
        c.execute("update groups set profiles_json=? where id=?", (json.dumps([profile_id]), gid))
        settings = {"remember_id": str(profile_id), "remember_enable": "true", "current_group": str(gid),
                    "remember_tun": "false", "remember_system_proxy": "false", "net_use_proxy": "false",
                    "core_dns_in_port": str(inbound_port), "inbound_socks_port": str(port()), "core_box_api_port": "-9091",
                    "remote_dns": upstream, "dns_final_out": "remote", "use_dns_object": "false", "fakedns": "false",
                    "sub_auto_update": "-30", "route_auto_update": "-30", "language": "1"}
        c.executemany("insert or replace into settings(key,value) values (?,?)", settings.items())
        c.commit()
    tcp_before, udp_before = tcp.calls, udp.calls
    process = start(exe, appdata)
    try:
        transaction = 0x7319
        question = b"".join(bytes([len(part)]) + part.encode() for part in "mihomo-dns.test".split(".")) + b"\0" + struct.pack("!HH", 1, 1)
        query = struct.pack("!HHHHHH", transaction, 0x0100, 1, 0, 0, 0) + question
        def answered():
            with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
                sock.settimeout(0.4)
                sock.sendto(query, ("127.0.0.1", inbound_port))
                try:
                    answer = sock.recv(4096)
                except OSError:
                    return False
                return answer[:2] == query[:2] and len(answer) >= 12 and (answer[3] & 15) == 0 and answer.endswith(socket.inet_aton("192.0.2.123"))
        try:
            wait_for(answered, process, 25)
        except RuntimeError:
            for log in config.rglob("*.log"):
                print(log.name, log.read_text(encoding="utf-8", errors="replace")[-6000:], flush=True)
            raise
        assert (tcp.calls > tcp_before) == expect_tcp, "Unexpected remote DNS TCP transport"
        assert (udp.calls > udp_before) != expect_tcp, "Unexpected remote DNS UDP transport"
        print(json.dumps({"case": label, "upstream_transport": "tcp" if expect_tcp else "udp", "dns_udp_inbound_answered": True}), flush=True)
    finally:
        stop(process)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--gui", type=Path, required=True)
    parser.add_argument("--core-dir", type=Path, required=True)
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="throne-dns-test-") as temporary:
        root = Path(temporary)
        runtime = root / "runtime"
        runtime.mkdir()
        exe = runtime / "Throne.exe"
        shutil.copy2(args.gui, exe)
        for name in ["ThroneCore.exe", "libcronet.dll"]:
            shutil.copy2(args.core_dir / name, runtime / name)
        initial = root / "initial"
        initial.mkdir()
        process = start(exe, initial)
        seed = initial / "config/throne.db"
        try:
            def initialized():
                if not seed.exists():
                    return False
                try:
                    with closing(sqlite3.connect(seed)) as c:
                        return c.execute("select count(*) from groups").fetchone()[0] > 0
                except sqlite3.Error:
                    return False
            wait_for(initialized, process)
            time.sleep(3)
        finally:
            stop(process)
        with TCPServer(("127.0.0.1", 0), TCPDNS) as tcp, UDPServer(tcp.server_address, UDPDNS) as udp, TCPServer(("127.0.0.1", 0), Socks) as socks:
            # Both protocols must use the same configured DNS address and port.
            tcp.calls = udp.calls = 0
            socks.allowed_port = tcp.server_address[1]
            for server in [tcp, udp, socks]:
                threading.Thread(target=server.serve_forever, daemon=True).start()
            try:
                upstream = "127.0.0.1:" + str(tcp.server_address[1])
                outbound = {"type": "mihomo", "server": "127.0.0.1", "server_port": socks.server_address[1],
                            "proxy": {"type": "socks5", "server": "127.0.0.1", "port": socks.server_address[1], "udp": False}}
                check(exe, root, seed, "tcp-only-mihomo", outbound, upstream, tcp, udp, True)
                outbound["proxy"]["udp"] = True
                check(exe, root, seed, "udp-mihomo-control", outbound, upstream, tcp, udp, False)
            finally:
                for server in [tcp, udp, socks]:
                    server.shutdown()


if __name__ == "__main__":
    main()
