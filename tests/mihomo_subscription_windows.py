"""Exercise actual GUI subscription HTTP requests in isolated Windows databases."""
import argparse
import base64
from contextlib import closing
import http.server
import json
from pathlib import Path
import shutil
import sqlite3
import subprocess
import tempfile
import threading
import time


PROXIES = [
    {"name": "Synthetic VLESS", "type": "vless", "server": "example.test", "port": 443,
     "uuid": "00000000-0000-4000-8000-000000000001", "tls": False},
    {"name": "Synthetic Sudoku", "type": "sudoku", "server": "example.test", "port": 7443,
     "key": "synthetic-test-key", "udp": True, "extension-options": {"nested": [False, 123]}},
    {"name": "Synthetic AnyTLS", "type": "anytls", "server": "example.test", "port": 2443,
     "password": "synthetic-test-password", "client-fingerprint": "chrome"},
]


class Provider(http.server.BaseHTTPRequestHandler):
    agents = []

    def do_GET(self):
        agent = self.headers.get("User-Agent", "")
        self.agents.append(agent)
        # Reproduce providers that choose a limited native format when Throne is present.
        if "Throne" in agent:
            body = base64.b64encode(b"vless://00000000-0000-4000-8000-000000000001@example.test:443#Limited\n"
                                    b"anytls://synthetic-test-password@example.test:2443?security=tls#Synthetic%20AnyTLS")
        else:
            body = json.dumps({"proxies": PROXIES}).encode()
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_):
        pass


def start(exe, appdata):
    info = subprocess.STARTUPINFO()
    info.dwFlags |= subprocess.STARTF_USESHOWWINDOW
    info.wShowWindow = 0
    return subprocess.Popen([str(exe), "-many", "-tray", "-appdata", str(appdata)],
                            cwd=exe.parent, startupinfo=info)


def stop(process):
    if process.poll() is None:
        process.terminate()
        process.wait(timeout=10)
    # ThroneCore exits when its parent closes; let it release the private database/runtime.
    time.sleep(1)


def wait_for(check, process, seconds=45):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise RuntimeError("Isolated GUI exited before the subscription check finished")
        if check():
            return
        time.sleep(0.25)
    raise RuntimeError("Isolated GUI subscription check timed out")


def snapshot(database, target):
    with closing(sqlite3.connect(f"file:{database.as_posix()}?mode=ro", uri=True)) as source:
        with closing(sqlite3.connect(target)) as dest:
            source.backup(dest)


def check_case(exe, root, seed, name, url, expected, global_agent="", group_agent="", want_agent=None, native=False):
    appdata = root / name
    config = appdata / "config"
    config.mkdir(parents=True)
    db = config / "throne.db"
    snapshot(seed, db)
    with closing(sqlite3.connect(db)) as c:
        gid = c.execute("select id from groups order by (url != '') desc,id limit 1").fetchone()[0]
        previous_ids = dict(c.execute("select name,id from profiles where gid=?", (gid,)))
        previous_types = dict(c.execute("select name,type from profiles where gid=?", (gid,)))
        settings = {"user_agent2": global_agent, "sub_auto_update": "30", "remember_id": "-1",
                    "spmode_system_proxy": "false", "spmode_vpn": "false", "net_use_proxy": "false",
                    "net_insecure": "false", "inbound_socks_port": "23080", "inbound_dns_port": "23053",
                    "core_box_clash_api": "-23091", "route_auto_update": "-30", "language": "1"}
        c.executemany("insert or replace into settings(key,value) values (?,?)", settings.items())
        c.execute("update groups set skip_auto_update=1")
        c.execute("update groups set url=?,name=?,sub_last_update=0,skip_auto_update=0,sub_options_json=? where id=?",
                  (url, name, json.dumps({"user_agent": group_agent}) if group_agent else "{}", gid))
        c.commit()
    before = len(Provider.agents)
    process = start(exe, appdata)
    try:
        def imported():
            try:
                with closing(sqlite3.connect(f"file:{db.as_posix()}?mode=ro", uri=True)) as c:
                    rows = c.execute("select type,outbound_json from profiles where gid=?", (gid,)).fetchall()
                    return len(rows) == len(expected) and all((t != "custom" if native else t == "custom") for t, _ in rows)
            except sqlite3.Error:
                return False
        wait_for(imported, process)
        if not native:
            with closing(sqlite3.connect(f"file:{db.as_posix()}?mode=ro", uri=True)) as c:
                actual = [json.loads(json.loads(r[0])["config"])["proxy"]
                          for r in c.execute("select outbound_json from profiles where gid=? order by id", (gid,))]
            assert sorted(actual, key=lambda p: p["name"]) == sorted(expected, key=lambda p: p["name"]), "Proxy fields changed during HTTP import"
        if want_agent is not None:
            assert Provider.agents[before:] == [want_agent], "Incorrect subscription User-Agent or unexpected extra request"
        result = {"case": name, "imported_profiles": len(expected), "passed": True}
        if name == "private-live-url":
            with closing(sqlite3.connect(db)) as c:
                current_ids = dict(c.execute("select name,id from profiles where gid=?", (gid,)))
            shared_names = previous_ids.keys() & current_ids.keys()
            changed = [{"previous_type": previous_types[n], "previous_id": previous_ids[n], "current_id": current_ids[n]}
                       for n in shared_names if previous_ids[n] != current_ids[n]]
            if changed:
                print(json.dumps({"case": name, "changed_previous_ids": changed}), flush=True)
            assert all(previous_ids[n] == current_ids[n] for n in shared_names), "Existing profile IDs changed during the real subscription update"
            result["retained_existing_ids"] = len(shared_names)
        print(json.dumps(result), flush=True)
    finally:
        stop(process)
    return db


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--gui", type=Path, required=True)
    parser.add_argument("--core-dir", type=Path, required=True)
    parser.add_argument("--private-url-db", type=Path)
    parser.add_argument("--expected-json", type=Path)
    parser.add_argument("--private-only", action="store_true")
    args = parser.parse_args()
    if args.private_only and not args.private_url_db:
        parser.error("--private-only requires --private-url-db")
    with tempfile.TemporaryDirectory(prefix="throne-subscription-test-") as temporary:
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
                    with closing(sqlite3.connect(f"file:{seed.as_posix()}?mode=ro", uri=True)) as c:
                        return c.execute("select count(*) from groups").fetchone()[0] > 0
                except sqlite3.Error:
                    return False
            wait_for(initialized, process)
            time.sleep(3)
        finally:
            stop(process)
        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Provider)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            url = f"http://127.0.0.1:{server.server_port}/subscription"
            if not args.private_only:
                check_case(exe, root, seed, "default-agent", url, PROXIES, want_agent="mihomo/1.19.32")
                check_case(exe, root, seed, "global-agent", url, PROXIES, global_agent="custom-global-client", want_agent="custom-global-client")
                check_case(exe, root, seed, "group-agent", url, PROXIES, global_agent="custom-global-client", group_agent="custom-group-client", want_agent="custom-group-client")
                legacy = check_case(exe, root, seed, "legacy-link-agent", url, [PROXIES[0], PROXIES[2]], global_agent="Throne/1.0.0", want_agent="Throne/1.0.0", native=True)
                with closing(sqlite3.connect(legacy)) as c:
                    legacy_id = c.execute("select id from profiles where type='vless'").fetchone()[0]
                    anytls_id = c.execute("select id from profiles where type='anytls'").fetchone()[0]
                migrated = check_case(exe, root, legacy, "legacy-format-update", url, PROXIES, want_agent="mihomo/1.19.32")
                with closing(sqlite3.connect(migrated)) as c:
                    migrated_id = c.execute("select id from profiles where name=?", (PROXIES[0]["name"],)).fetchone()[0]
                    assert migrated_id == legacy_id, "Format update changed a profile ID referenced by routing"
                    assert c.execute("select id from profiles where name=?", (PROXIES[2]["name"],)).fetchone()[0] == anytls_id, "Format update changed an AnyTLS profile ID"
                xray_seed = root / "xray-seed.db"
                snapshot(legacy, xray_seed)
                with closing(sqlite3.connect(xray_seed)) as c:
                    xray = {"protocol": "vless", "tag": "Limited", "settings": {
                        "address": PROXIES[0]["server"], "port": PROXIES[0]["port"],
                        "id": PROXIES[0]["uuid"], "encryption": "none"}, "streamSettings": {"network": "tcp"}}
                    c.execute("update profiles set type='xrayvless',outbound_json=? where id=?", (json.dumps(xray), legacy_id))
                    c.commit()
                migrated = check_case(exe, root, xray_seed, "xray-format-update", url, PROXIES, want_agent="mihomo/1.19.32")
                with closing(sqlite3.connect(migrated)) as c:
                    migrated_id = c.execute("select id from profiles where name=?", (PROXIES[0]["name"],)).fetchone()[0]
                    assert migrated_id == legacy_id, "Xray format update changed a profile ID referenced by routing"
            if args.private_url_db:
                assert args.expected_json, "Private URL check requires expected JSON"
                with closing(sqlite3.connect(f"file:{args.private_url_db.resolve().as_posix()}?mode=ro", uri=True)) as c:
                    url = c.execute("select url from groups where url != '' limit 1").fetchone()[0]
                expected = json.loads(args.expected_json.read_text(encoding="utf-8-sig"))["proxies"]
                check_case(exe, root, args.private_url_db.resolve(), "private-live-url", url, expected)
        finally:
            server.shutdown()


if __name__ == "__main__":
    main()
