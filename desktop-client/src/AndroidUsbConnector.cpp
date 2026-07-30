#include "AndroidUsbConnector.h"

#include <QDir>
#include <QFileInfo>
#include <QInputDialog>
#include <QProcess>
#include <QRandomGenerator>
#include <QRegularExpression>
#include <QStandardPaths>
#include <QTimer>

namespace {
constexpr int BRIDGE_PORT = 39471;

QString executableName() {
#ifdef Q_OS_WIN
    return QStringLiteral("adb.exe");
#else
    return QStringLiteral("adb");
#endif
}

QString randomToken() {
    QByteArray bytes(32, '\0');
    QRandomGenerator *generator = QRandomGenerator::system();
    for (char &byte : bytes) {
        byte = static_cast<char>(generator->generate() & 0xffU);
    }
    return QString::fromLatin1(bytes.toHex());
}

QString shellQuote(QString value) {
    value.replace(QLatin1Char('\''), QStringLiteral("'\\''"));
    return QStringLiteral("'") + value + QStringLiteral("'");
}
}

AndroidUsbConnector::AndroidUsbConnector(QObject *parent)
    : QObject(parent), process_(new QProcess(this)), timeout_(new QTimer(this)) {
    timeout_->setSingleShot(true);
    timeout_->setInterval(15000);
    connect(timeout_, &QTimer::timeout, this, [this] {
        process_->kill();
        operation_ = Operation::None;
        setStatus(QStringLiteral("ADB 操作超时，请重新插拔 USB 后重试"));
        emit activationFinished(false, status_);
    });
    connect(process_, &QProcess::finished, this,
            [this](int exitCode, QProcess::ExitStatus) {
                timeout_->stop();
                handleFinished(exitCode);
            });
}

QString AndroidUsbConnector::status() const { return status_; }

QStringList AndroidUsbConnector::parseAuthorizedSerials(const QByteArray &output) {
    QStringList result;
    const QList<QByteArray> lines = output.split('\n');
    for (const QByteArray &rawLine : lines) {
        const QByteArray line = rawLine.trimmed();
        if (line.isEmpty() || line.startsWith("List of devices")) continue;
        const QList<QByteArray> columns = line.simplified().split(' ');
        if (columns.size() >= 2 && columns.at(1) == "device") {
            result.push_back(QString::fromUtf8(columns.first()));
        }
    }
    return result;
}

void AndroidUsbConnector::refresh() {
    activateAfterDetection_ = false;
    adbPath_ = findAdb();
    if (adbPath_.isEmpty()) {
        setStatus(QStringLiteral("未找到 ADB，请安装 Android Platform Tools"));
        return;
    }
    setStatus(QStringLiteral("正在检测 USB 手机…"));
    start(Operation::Detect, {QStringLiteral("devices"), QStringLiteral("-l")});
}

void AndroidUsbConnector::activateClipBridge() {
    adbPath_ = findAdb();
    if (adbPath_.isEmpty()) {
        const QString message =
            QStringLiteral("未找到 ADB，请安装 Android Platform Tools 或配置 ANDROID_HOME");
        setStatus(message);
        emit activationFinished(false, message);
        return;
    }
    activateAfterDetection_ = true;
    setStatus(QStringLiteral("正在检测并授权 USB 手机…"));
    start(Operation::Detect, {QStringLiteral("devices"), QStringLiteral("-l")});
}

void AndroidUsbConnector::start(Operation operation, const QStringList &arguments) {
    if (process_->state() != QProcess::NotRunning) {
        process_->kill();
        process_->waitForFinished(1000);
    }
    operation_ = operation;
    process_->setProgram(adbPath_);
    process_->setArguments(arguments);
    process_->start();
    timeout_->start();
}

void AndroidUsbConnector::handleFinished(int exitCode) {
    const Operation finishedOperation = operation_;
    operation_ = Operation::None;
    const QByteArray output =
        process_->readAllStandardOutput() + process_->readAllStandardError();
    // 超时处理已经给出明确提示；kill() 触发的 finished 不应再次覆盖它。
    if (finishedOperation == Operation::None) return;
    if (exitCode != 0 && finishedOperation != Operation::StopOldBridge) {
        const QString message = QStringLiteral("ADB 执行失败：%1")
                                    .arg(QString::fromUtf8(output).trimmed().left(240));
        setStatus(message);
        emit activationFinished(false, message);
        return;
    }

    if (finishedOperation == Operation::Detect) {
        const QStringList serials = parseAuthorizedSerials(output);
        if (serials.isEmpty()) {
            const bool unauthorized = output.contains("unauthorized");
            const QString message =
                unauthorized
                    ? QStringLiteral("手机未授权，请解锁手机并确认“允许 USB 调试”")
                    : QStringLiteral("未发现已授权的 USB 手机");
            setStatus(message);
            if (activateAfterDetection_) emit activationFinished(false, message);
            return;
        }
        if (serials.size() > 1 && activateAfterDetection_) {
            bool accepted = false;
            const QString selected = QInputDialog::getItem(
                nullptr, QStringLiteral("选择 USB 设备"),
                QStringLiteral("检测到多台 Android 设备，请选择需要恢复后台互通的一台："),
                serials, 0, false, &accepted);
            if (!accepted || selected.isEmpty()) {
                activateAfterDetection_ = false;
                const QString message = QStringLiteral("已取消 USB 设备选择");
                setStatus(message);
                emit activationFinished(false, message);
                return;
            }
            serial_ = selected;
        } else {
            serial_ = serials.first();
        }
        setStatus(QStringLiteral("已连接手机：%1").arg(serial_));
        if (activateAfterDetection_) {
            activateAfterDetection_ = false;
            setStatus(QStringLiteral("正在检查手机 ClipBridge…"));
            start(Operation::CheckClipBridgePackage,
                  {QStringLiteral("-s"), serial_, QStringLiteral("shell"),
                   QStringLiteral("pm"), QStringLiteral("path"),
                   QStringLiteral("com.clipbridge.app")});
        }
        return;
    }

    if (finishedOperation == Operation::CheckClipBridgePackage) {
        const QString packageOutput = QString::fromUtf8(output).trimmed();
        const QRegularExpression pathPattern(
            QStringLiteral(R"(package:(/data/app/[A-Za-z0-9_~=/+.\-]+/base\.apk))"));
        const auto match = pathPattern.match(packageOutput);
        if (exitCode != 0 || !match.hasMatch()) {
            const QString message =
                QStringLiteral("当前 USB 手机未安装 ClipBridge，请先安装最新版 APK");
            setStatus(message);
            emit activationFinished(false, message);
            return;
        }
        apkPath_ = match.captured(1);
        token_ = randomToken();
        setStatus(QStringLiteral("正在停止旧的剪贴板桥接…"));
        start(Operation::StopOldBridge,
              {QStringLiteral("-s"), serial_, QStringLiteral("shell"),
               QStringLiteral("pkill"), QStringLiteral("-f"),
               QStringLiteral("clipbridge_adb_bridge")});
        return;
    }

    if (finishedOperation == Operation::StopOldBridge) {
        setStatus(QStringLiteral("正在启动 ClipBridge ADB 桥接…"));
        const QString command =
            QStringLiteral("CLASSPATH=%1 nohup app_process /system/bin "
                           "--nice-name=clipbridge_adb_bridge "
                           "com.clipbridge.app.clipboard.AdbClipboardBridgeMain "
                           "%2 %3 >/dev/null 2>&1 &")
                .arg(shellQuote(apkPath_), token_)
                .arg(BRIDGE_PORT);
        start(Operation::StartBridge,
              {QStringLiteral("-s"), serial_, QStringLiteral("shell"),
               command});
        return;
    }

    if (finishedOperation == Operation::StartBridge) {
        setStatus(QStringLiteral("ADB 桥接已启动，正在打开 ClipBridge…"));
        start(Operation::LaunchApp,
              {QStringLiteral("-s"), serial_, QStringLiteral("shell"),
               QStringLiteral("am"), QStringLiteral("start"),
               QStringLiteral("-n"),
               QStringLiteral("com.clipbridge.app/.MainActivity"),
               QStringLiteral("--es"), QStringLiteral("clipbridge_adb_token"),
               token_, QStringLiteral("--ei"),
               QStringLiteral("clipbridge_adb_port"),
               QString::number(BRIDGE_PORT)});
        return;
    }

    if (finishedOperation == Operation::LaunchApp) {
        setStatus(QStringLiteral("正在验证 ADB 桥接进程…"));
        start(Operation::VerifyBridge,
              {QStringLiteral("-s"), serial_, QStringLiteral("shell"),
               QStringLiteral("pidof"),
               QStringLiteral("clipbridge_adb_bridge")});
        return;
    }

    if (finishedOperation == Operation::VerifyBridge) {
        if (QString::fromUtf8(output).trimmed().isEmpty()) {
            const QString message =
                QStringLiteral("ADB 桥接启动失败，请确认手机系统允许 USB 调试");
            setStatus(message);
            emit activationFinished(false, message);
            return;
        }
        const QString message = QStringLiteral("手机互通环境已恢复");
        setStatus(message);
        emit activationFinished(true, message);
    }
}

void AndroidUsbConnector::setStatus(const QString &status) {
    if (status_ == status) return;
    status_ = status;
    emit statusChanged(status_);
}

QString AndroidUsbConnector::findAdb() const {
    const QString fromPath = QStandardPaths::findExecutable(executableName());
    if (!fromPath.isEmpty()) return fromPath;

    QStringList candidates;
    for (const char *variable : {"ANDROID_HOME", "ANDROID_SDK_ROOT"}) {
        const QString root = qEnvironmentVariable(variable);
        if (!root.isEmpty()) {
            candidates.push_back(
                QDir(root).filePath(QStringLiteral("platform-tools/") + executableName()));
        }
    }
#ifdef Q_OS_WIN
    const QString localAppData = qEnvironmentVariable("LOCALAPPDATA");
    if (!localAppData.isEmpty()) {
        candidates.push_back(QDir(localAppData).filePath(
            QStringLiteral("Android/Sdk/platform-tools/adb.exe")));
    }
#else
    candidates.push_back(QDir::home().filePath(
        QStringLiteral("Android/Sdk/platform-tools/adb")));
    candidates.push_back(QDir::home().filePath(
        QStringLiteral("android-sdk/platform-tools/adb")));
#endif
    for (const QString &candidate : candidates) {
        const QFileInfo info(candidate);
        if (info.isFile() && info.isExecutable()) return info.absoluteFilePath();
    }
    return {};
}
