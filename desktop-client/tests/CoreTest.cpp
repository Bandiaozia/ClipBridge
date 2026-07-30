#include "AndroidUsbConnector.h"
#include "ClipboardManager.h"
#include "CryptoManager.h"
#include "HistoryRepository.h"
#include "SensitiveContentDetector.h"
#include "SettingsRepository.h"

#include <QDateTime>
#include <QStandardPaths>
#include <QTest>

class CoreTest final : public QObject {
    Q_OBJECT

private slots:
    void initTestCase() {
        QStandardPaths::setTestModeEnabled(true);
    }

    void detectsSensitiveContent() {
        SensitiveContentDetector detector;
        QVERIFY(detector.isSensitive(
            QStringLiteral("Authorization: Bearer abcdefghijklmnopqrstuvwxyz")));
        QVERIFY(detector.isSensitive(
            QStringLiteral("postgresql://alice:secret@example/db")));
        QVERIFY(!detector.isSensitive(QStringLiteral("普通的剪贴板文本")));
    }

    void clipboardDedupWindowCoversX11OwnershipChanges() {
        // X11 剪贴板所有者退出时常在数秒后重新触发 dataChanged。
        // 窗口至少十秒，才能覆盖所有权转移而又不长期阻止用户再次复制。
        QVERIFY(ClipboardManager::duplicateWindowMs() >= 10000);
    }

    void parsesOnlyAuthorizedAdbDevices() {
        const QByteArray output(
            "List of devices attached\n"
            "ABC123 device product:test model:Phone transport_id:1\n"
            "WAITING unauthorized usb:1-1 transport_id:2\n"
            "OFFLINE offline transport_id:3\n");
        QCOMPARE(AndroidUsbConnector::parseAuthorizedSerials(output),
                 QStringList{QStringLiteral("ABC123")});
    }

    void persistsSelectedTargetDevices() {
        SettingsRepository settings;
        const QStringList ids{
            QStringLiteral("44444444-4444-4444-8444-444444444444"),
        };
        settings.setTargetDeviceIds(ids);
        QCOMPARE(settings.targetDeviceIds(), ids);
        settings.setTargetDeviceIds({});
    }

    void persistsDesktopThemeColor() {
        SettingsRepository settings;
        settings.setThemeColor(QStringLiteral("green"));
        QCOMPARE(settings.themeColor(), QStringLiteral("green"));
        settings.setThemeColor(QStringLiteral("blue"));
    }

    void encryptsAndAuthenticatesText() {
        CryptoManager crypto;
        QString error;
        QVERIFY2(crypto.initialize(&error), qPrintable(error));
        Device peer;
        peer.id = QStringLiteral("22222222-2222-4222-8222-222222222222");
        peer.x25519PublicKey = crypto.x25519PublicKey();
        peer.ed25519PublicKey = crypto.ed25519PublicKey();
        const QString senderId =
            QStringLiteral("11111111-1111-4111-8111-111111111111");
        const Envelope envelope =
            crypto.encryptText(QStringLiteral("加密测试"), senderId, peer, 60000,
                               true, &error);
        QVERIFY2(!envelope.messageId.isEmpty(), qPrintable(error));

        Device sender = peer;
        sender.id = senderId;
        QCOMPARE(crypto.decryptText(envelope, sender, &error),
                 QStringLiteral("加密测试"));
        QVERIFY2(error.isEmpty(), qPrintable(error));

        Envelope tampered = envelope;
        tampered.ciphertext[0] ^= 1;
        error.clear();
        QVERIFY(crypto.decryptText(tampered, sender, &error).isEmpty());
        QVERIFY(!error.isEmpty());
    }

    void storesAndDeduplicatesHistory() {
        HistoryRepository history;
        QString error;
        QVERIFY2(history.open(&error), qPrintable(error));
        history.clear();
        HistoryRecord record;
        record.messageId =
            QStringLiteral("33333333-3333-4333-8333-333333333333");
        record.content = QStringLiteral("history text");
        record.contentHash = QByteArray(32, 'x');
        record.sourceDevice = QStringLiteral("source");
        record.targetDevice = QStringLiteral("target");
        record.createdAt = QDateTime::currentMSecsSinceEpoch();
        record.receivedAt = record.createdAt;
        QVERIFY(history.add(record, &error));
        QVERIFY(history.add(record, &error));
        QVERIFY(history.containsMessage(record.messageId));
        QCOMPARE(history.search(QStringLiteral("history")).size(), 1);
        QVERIFY(history.clear());
    }
};

QTEST_MAIN(CoreTest)
#include "CoreTest.moc"
#include "AndroidUsbConnector.h"
