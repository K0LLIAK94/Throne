#pragma once

#include "3rdparty/fkYAML/node.hpp"
#include <QJsonArray>
#include <QJsonObject>
#include <stdexcept>

namespace Subscription::Mihomo {
    // Preserve extension fields and scalar types instead of translating through
    // the fixed Clash schema. New protocol options must reach the adapter intact.
    inline QJsonValue yamlValue(const fkyaml::node &node, int depth = 0) {
        if (depth > 32) throw std::runtime_error("Mihomo proxy nesting limit exceeded");
        if (node.is_mapping()) {
            QJsonObject object;
            for (const auto &[key, value] : node.as_map()) {
                if (!key.is_string()) throw std::runtime_error("Mihomo proxy keys must be strings");
                object[QString::fromStdString(key.as_str())] = yamlValue(value, depth + 1);
            }
            return object;
        }
        if (node.is_sequence()) {
            QJsonArray array;
            for (const auto &value : node.as_seq()) array.append(yamlValue(value, depth + 1));
            return array;
        }
        if (node.is_null()) return QJsonValue(QJsonValue::Null);
        if (node.is_boolean()) return node.as_bool();
        if (node.is_integer()) return QJsonValue(static_cast<qint64>(node.as_int()));
        if (node.is_float_number()) return node.as_float();
        return QString::fromStdString(node.as_str());
    }

    inline QJsonObject wrapProxy(const QJsonObject &proxy) {
        const auto type = proxy["type"].toString();
        // Subscription routing, groups and direct/reject nodes never replace
        // Throne's routing configuration.
        if (type.isEmpty() || type == "direct" || type == "reject" || type == "reject-drop"
            || type == "selector" || type == "url-test" || type == "fallback" || type == "load-balance") return {};
        if (proxy["server"].toString().isEmpty()) return {};
        const auto port = proxy["port"].toInt();
        if (port < 1 || port > 65535) return {};
        return {{"type", "mihomo"}, {"server", proxy["server"]}, {"server_port", port}, {"proxy", proxy}};
    }
}
