#include "include/configs/sub/MihomoProxy.hpp"
#include <QJsonDocument>
#include <QFile>
#include <iostream>

int main(int argc, char **argv) {
    using namespace Subscription::Mihomo;
    const auto source = fkyaml::node::deserialize(R"(
proxies:
  - name: Test Sudoku
    type: sudoku
    server: example.test
    port: 7443
    key: 'synthetic-test-key'
    aead-method: chacha20-poly1305
    padding-min: 1
    padding-max: 9
    extension-options:
      boolean: true
      text: '00123'
      large-integer: 9007199254740993
      nested: [null, false, 1.5]
    udp: true
  - name: Test XHTTP
    type: vless
    server: example.test
    port: 443
    encryption: 'synthetic-encryption-option'
    xhttp-opts:
      path: /test
      extra:
        downloadSettings:
          address: download.example.test
)");
    const auto array = yamlValue(source["proxies"]).toArray();
    const auto first = array.first().toObject();
    const auto wrapped = wrapProxy(first);
    const auto roundTrip = QJsonDocument::fromJson(QJsonDocument(wrapped).toJson()).object();
    if (roundTrip["proxy"].toObject() != first || wrapped["server_port"].toInt() != 7443) return 1;
    const auto ext = first["extension-options"].toObject();
    if (!ext["boolean"].toBool() || ext["text"].toString() != "00123"
        || ext["large-integer"].toInteger() != 9007199254740993LL || !ext["nested"].toArray().first().isNull()) return 2;
    if (wrapProxy(array.last().toObject())["proxy"].toObject()["xhttp-opts"] != array.last().toObject()["xhttp-opts"]) return 3;
    if (!wrapProxy(QJsonObject{{"type", "direct"}, {"server", "example.test"}, {"port", 443}}).isEmpty()) return 4;
    if (!wrapProxy(QJsonObject{{"type", "sudoku"}, {"server", "example.test"}, {"port", 65536}}).isEmpty()) return 5;
    // Optional private subscription verification; input content is never printed.
    if (argc > 1) {
        QFile file(QString::fromLocal8Bit(argv[1]));
        if (!file.open(QIODevice::ReadOnly)) return 6;
        const auto proxies = QJsonDocument::fromJson(file.readAll()).object()["proxies"].toArray();
        if (proxies.isEmpty()) return 7;
        for (const auto &proxy : proxies) {
            const auto input = proxy.toObject();
            const auto result = wrapProxy(input);
            if (result.isEmpty() || result["proxy"].toObject() != input) return 8;
        }
        std::cout << "Preserved all " << proxies.size() << " subscription profiles\n";
    }
    std::cout << "Mihomo import checks passed\n";
    return 0;
}
