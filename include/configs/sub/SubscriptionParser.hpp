#pragma once

#include "include/database/entities/Profile.h"

#include <QByteArray>
#include <QString>

#include <functional>
#include <memory>

namespace Subscription {
    struct ParseSink {
        std::function<void(std::shared_ptr<Configs::Profile>)> profile;
        std::function<void(const QString &)> log;
        std::function<void(const QString &, const QString &)> warn;
    };

    // Profiles are handed to the sink in document order; the body is consumed in place.
    void ParseDocument(QByteArray body, const ParseSink &sink);

    void ParseText(const QString &text, const ParseSink &sink);

    // Recreate only the old importer's identity when migrating an existing
    // Clash profile. Runtime configuration always keeps the complete raw proxy.
    std::shared_ptr<Configs::Profile> LegacyMihomoProfile(const QJsonObject &proxy);
}
