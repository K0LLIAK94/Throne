# Throne — Mihomo fork

Неофициальный форк [throneproj/Throne](https://github.com/throneproj/Throne) с дополнительной поддержкой протоколов через **Mihomo v1.19.32**. Сохраняет привычный интерфейс Throne: маршрутизацией, DNS, TUN и цепочками управляет sing-box, а встроенные адаптеры Mihomo подключаются к прокси внутри ThroneCore.

### Что добавлено в форке

- Импорт Clash/Mihomo подписок в JSON и YAML с сохранением параметров прокси.
- Дополнительные варианты протоколов и транспортов: VLESS XHTTP, Hysteria2 Gecko, ShadowQUIC, Sudoku и другие адаптеры включённой версии Mihomo.
- Исправлен запрос полного формата подписки и перенос существующих профилей при смене формата.
- DNS через TCP для профилей Mihomo с отключённым UDP, включая запросы из TUN.

Правила и группы из подписки не заменяют маршрутизацию Throne. Доступность UDP зависит от параметров профиля и сервера. Подробности: [Mihomo support](docs/mihomo.md).

### Скачать и запустить

**[Релизы этого форка](https://github.com/K0LLIAK94/Throne/releases/latest)** — portable ZIP для Windows x64.

1. Распакуйте архив целиком в отдельную папку.
2. Запустите `Throne.exe`; `ThroneCore.exe` и `libcronet.dll` должны лежать рядом.
3. Добавьте подписку обычным способом. Если её список неполный, проверьте заданный вручную User-Agent в настройках приложения и группы.

Для переноса настроек закройте старый Throne и скопируйте его папку `config` в новую папку программы, сохранив резервную копию. Архив релиза содержит только программу и документацию.

Исходный проект поддерживает Windows, Linux и macOS. В этом форке сейчас опубликована сборка Windows x64; инструкции для других платформ ниже относятся к исходному Throne.

Qt based Desktop cross-platform GUI proxy utility, powered by [sing-box](https://github.com/SagerNet/sing-box) and [Mihomo](https://github.com/MetaCubeX/mihomo).

<img width="1002" height="789" alt="image" src="https://github.com/user-attachments/assets/af4a8e32-7e55-430c-9402-ec2d665cf71a" />

### Note on MacOS releases
Apple platforms have a very strict security policy and since Throne does not have a signed certificate, you will have to remove the quarantine using `xattr -d com.apple.quarantine /path/to/throne.app`. Move `Throne.app` to `/Applications` before the first launch — the built-in privilege escalation opens `Terminal` to make the core setuid-root, and that step can fail while the app is still inside `~/Downloads`.

### GitHub Releases (Portable ZIP)

[![GitHub All Releases](https://img.shields.io/github/downloads/K0LLIAK94/Throne/total?label=downloads-total&logo=github&style=flat-square)](https://github.com/K0LLIAK94/Throne/releases)

# Upstream Linux CLI installer
```bash
curl -fsSL https://raw.githubusercontent.com/throneproj/Throne/dev/script/install_linux.py | sudo python3
```

### RPM repository
[Throne RPM repository](https://parhelia512.github.io/) for Fedora/RHEL and openSUSE/SLE.

## Supported protocols

- SOCKS
- HTTP(S)
- Shadowsocks
- Trojan
- VMess
- VLESS
- TUIC
- Hysteria
- Hysteria2
- AnyTLS
- Mieru
- Snell
- NaïveProxy
- Juicity
- TrustTunnel
- ShadowTLS
- Wireguard
- AmneziaWG
- MASQUE
- SSH
- Xray VLESS
- OpenVPN/OpenConnect
- Custom Outbound (Both Sing-box and Xray)
- Custom Config (Both Sing-box and Xray)
- Chaining outbounds
- Extra Core

## Subscription Formats

Various formats are supported, including share links, JSON representations of sing-box configs, v2rayN links, and Clash/Mihomo JSON and YAML subscriptions. This fork retains complete Mihomo proxy definitions; subscription rules and proxy groups do not replace Throne's routing configuration.

Deeplinks are also supported, read the [documentation](https://throneproj.github.io/advanced/deeplinks/) for more information.

## Credits

- [throneproj/Throne](https://github.com/throneproj/Throne) — original project
- [MetaCubeX/mihomo](https://github.com/MetaCubeX/mihomo)
- [SagerNet/sing-box](https://github.com/SagerNet/sing-box)
- [XTLS/Xray-core](https://github.com/xtls/xray-core)
- [Qv2ray](https://github.com/Qv2ray/Qv2ray)
- [Qt](https://www.qt.io/)
- [simple-protobuf](https://github.com/tonda-kriz/simple-protobuf)
- [fkYAML](https://github.com/fktn-k/fkYAML)
- [quirc](https://github.com/dlbeer/quirc)
- [srombauts/sqlitecpp](https://github.com/srombauts/sqlitecpp)

## FAQ
**How does this project differ from the original Nekoray?** <br/>
Nekoray's developer partially abandoned the project on December of 2023, some minor updates were done recently but the project is now officially archived. This project was meant to continue the way of the original project, with lots of improvements, tons of new features and also, removal of obsolete features and simplifications.

**Why does my Anti-Virus detect Throne and/or its Core as malware?** <br/>
Throne's built-in update functionallity downloads the new release, removes the old files and replaces them with the new ones, which is quite simliar to what malwares do, remove your files and replace them with an encrypted version of your files.

**Is setting the `SUID` bit really needed on Linux?** <br/>
To create and manage a system TUN interface, root access is required, without it, you will have to grant the Core some `Cap_xxx_admin` and still, need to enter your password 3 to 4 times per TUN activation. You can also opt to disable the automatic privilege escalation in `Basic Settings`->`Security`, but note that features that require root access will stop working unless you manually grant the needed permissions.

**Why does my internet stop working after I force quit Throne?** <br/>
If Throne is force-quit while `System proxy` is enabled, the process ends immediately and Throne cannot reset the proxy. <br/>
Solution:
- Always close Throne normally.
- If you force quit by accident, open Throne again, enable `System proxy`, then disable it- this will reset the settings.

**Where are the downloadable route profiles/rulesets coming from?**<br/>
They are located at the [routeprofiles](https://github.com/throneproj/routeprofiles) repository.

**How does "Throne-\<version\>-debian-system-qt-x64.deb" differ from "Throne-\<version\>-debian-x64.deb" and why is the latter 3 times heavier then the former?**<br/>
The first one does not pack the Qt libraries and relies on those installed on the host. The second one packs everything needed with itself, thus being heavier. The reason the first one exists is that on legacy systems provided Qt libraries use unsupported system features. If a graphical interface fails to load for your system, you may try to download the system-qt version and install fitting Qt libraries from your package manager or compile them from source.
