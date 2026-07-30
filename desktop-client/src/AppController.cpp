#include "AppController.h"

#include "ApiClient.h"
#include "AndroidUsbConnector.h"
#include "AutoStartManager.h"
#include "ClipboardManager.h"
#include "ClipboardSyncService.h"
#include "CryptoManager.h"
#include "DeviceManager.h"
#include "HistoryRepository.h"
#include "NotificationManager.h"
#include "PairingManager.h"
#include "SensitiveContentDetector.h"
#include "SettingsRepository.h"
#include "TokenManager.h"
#include "WebSocketClient.h"

#include <QApplication>
#include <QCheckBox>
#include <QComboBox>
#include <QDateTime>
#include <QDialog>
#include <QDialogButtonBox>
#include <QFormLayout>
#include <QFrame>
#include <QGridLayout>
#include <QHeaderView>
#include <QHBoxLayout>
#include <QLabel>
#include <QLineEdit>
#include <QListWidget>
#include <QMainWindow>
#include <QMessageBox>
#include <QMouseEvent>
#include <QPixmap>
#include <QPalette>
#include <QPushButton>
#include <QSignalBlocker>
#include <QSpinBox>
#include <QStatusBar>
#include <QStyle>
#include <QTableWidget>
#include <QTextEdit>
#include <QUrl>
#include <QVBoxLayout>

namespace {

class WindowDragHeader final : public QFrame {
public:
    WindowDragHeader(QWidget *parent) : QFrame(parent) {
        setCursor(Qt::SizeAllCursor);
    }

protected:
    void mousePressEvent(QMouseEvent *event) override {
        if (event->button() == Qt::LeftButton)
            dragOffset_ = event->globalPosition().toPoint() - window()->pos();
    }

    void mouseMoveEvent(QMouseEvent *event) override {
        if (event->buttons() & Qt::LeftButton)
            window()->move(event->globalPosition().toPoint() - dragOffset_);
    }

private:
    QPoint dragOffset_;
};

class WindowResizeHandle final : public QLabel {
public:
    WindowResizeHandle(QWidget *window, QWidget *parent)
        : QLabel(QStringLiteral("◢"), parent), window_(window) {
        setAlignment(Qt::AlignCenter);
        setFixedSize(24, 24);
        setCursor(Qt::SizeFDiagCursor);
        setToolTip(QStringLiteral("拖动调整窗口大小"));
    }

protected:
    void mousePressEvent(QMouseEvent *event) override {
        if (event->button() == Qt::LeftButton) {
            startGlobal_ = event->globalPosition().toPoint();
            startSize_ = window_->size();
        }
    }

    void mouseMoveEvent(QMouseEvent *event) override {
        if (event->buttons() & Qt::LeftButton) {
            window_->resize(
                startSize_.width() + event->globalPosition().toPoint().x() - startGlobal_.x(),
                startSize_.height() + event->globalPosition().toPoint().y() - startGlobal_.y());
        }
    }

private:
    QWidget *window_;
    QPoint startGlobal_;
    QSize startSize_;
};

QString themeAccent(const QString &theme, bool dark) {
    if (theme == QStringLiteral("green")) {
        return dark ? QStringLiteral("#5F9672") : QStringLiteral("#44765B");
    }
    if (theme == QStringLiteral("purple")) {
        return dark ? QStringLiteral("#8575B2") : QStringLiteral("#62558C");
    }
    if (theme == QStringLiteral("orange")) {
        return dark ? QStringLiteral("#AD7954") : QStringLiteral("#8C5D3C");
    }
    if (theme == QStringLiteral("neutral")) {
        return dark ? QStringLiteral("#858B96") : QStringLiteral("#555B66");
    }
    return dark ? QStringLiteral("#6F8DBB") : QStringLiteral("#3F5F90");
}

QFrame *card(QWidget *parent) {
    auto *frame = new QFrame(parent);
    frame->setObjectName(QStringLiteral("card"));
    return frame;
}

}

AppController::AppController(QObject *parent)
    : QObject(parent),
      settings_(new SettingsRepository),
      autoStart_(new AutoStartManager),
      androidUsb_(new AndroidUsbConnector(this)),
      crypto_(new CryptoManager(this)),
      devices_(new DeviceManager(this)),
      history_(new HistoryRepository),
      tokens_(new TokenManager),
      api_(new ApiClient(settings_, devices_, crypto_, this)),
      pairing_(new PairingManager(api_, settings_, devices_, crypto_, this)),
      websocket_(new WebSocketClient(settings_, this)),
      clipboard_(new ClipboardManager(this)),
      sensitive_(new SensitiveContentDetector),
      sync_(new ClipboardSyncService(clipboard_, crypto_, devices_, history_,
                                     sensitive_, settings_, websocket_, this)) {
    sync_->setTargetDeviceIds(settings_->targetDeviceIds());
    QString error;
    if (!crypto_->initialize(&error)) {
        connectionStatus_ = error;
    }
    if (!history_->open(&error)) {
        connectionStatus_ = QStringLiteral("历史数据库错误：") + error;
    } else {
        history_->cleanup(settings_->retentionDays(), settings_->maxHistoryItems());
    }

    connect(api_, &ApiClient::authenticated, this, [this](const TokenPair &pair) {
        QString error;
        if (!tokens_->setTokens(pair, &error)) {
            QMessageBox::critical(window_, QStringLiteral("保存凭据失败"), error);
            return;
        }
        for (QWidget *widget : QApplication::topLevelWidgets()) {
            if (auto *dialog = qobject_cast<QDialog *>(widget);
                dialog && dialog->objectName() == QStringLiteral("loginDialog")) {
                dialog->accept();
            }
        }
        beginSession();
    });
    connect(api_, &ApiClient::refreshed, this, [this](const TokenPair &pair) {
        QString error;
        if (tokens_->setTokens(pair, &error)) {
            websocket_->updateToken(pair.accessToken);
        } else {
            setConnectionStatus(error);
        }
    });
    connect(api_, &ApiClient::devicesLoaded, this,
            [this](const QVector<Device> &list) {
                devices_->replaceDevices(list);
                refreshDeviceList();
            });
    connect(api_, &ApiClient::requestFailed, this, [this](const QString &message) {
        setConnectionStatus(QStringLiteral("请求失败：") + message);
    });
    connect(websocket_, &WebSocketClient::stateChanged, this,
            &AppController::setConnectionStatus);
    connect(websocket_, &WebSocketClient::authenticated, this, [this] {
        if (notifications_) notifications_->setConnected(true);
        api_->fetchDevices(tokens_->tokens().accessToken);
    });
    connect(websocket_, &WebSocketClient::deviceOnlineChanged, devices_,
            &DeviceManager::setOnline);
    connect(websocket_, &WebSocketClient::deviceRevoked, devices_,
            &DeviceManager::revoke);
    connect(devices_, &DeviceManager::devicesChanged, this,
            &AppController::refreshDeviceList);
    connect(sync_, &ClipboardSyncService::statusMessage, this,
            [this](const QString &message) {
                if (window_) window_->statusBar()->showMessage(message, 5000);
            });
    connect(sync_, &ClipboardSyncService::remoteTextReceived, this,
            [this](const QString &text, const QString &source, bool sensitive) {
                recentText_->setPlainText(sensitive
                                              ? QStringLiteral("疑似敏感内容，未自动显示")
                                              : text);
                if (notifications_) {
                    notifications_->showClipboardMessage(source, text, sensitive);
                }
            });
    connect(sync_, &ClipboardSyncService::sensitiveConfirmationRequired, this,
            [this](const QString &text, const QStringList &reasons) {
                const auto answer = QMessageBox::warning(
                    window_, QStringLiteral("疑似敏感内容"),
                    QStringLiteral("检测到：%1\n\n仅发送一次且不保存历史？")
                        .arg(reasons.join(QStringLiteral("、"))),
                    QMessageBox::Yes | QMessageBox::No, QMessageBox::No);
                if (answer == QMessageBox::Yes) sync_->sendText(text, true);
            });

    tokenRefreshTimer_.setInterval(30000);
    connect(&tokenRefreshTimer_, &QTimer::timeout, this, [this] {
        const TokenPair pair = tokens_->tokens();
        if (tokens_->needsRefresh(QDateTime::currentMSecsSinceEpoch())) {
            api_->refresh(pair.refreshToken);
        }
    });
}

AppController::~AppController() {
    websocket_->disconnectFromServer();
    history_->close();
    delete sensitive_;
    delete tokens_;
    delete history_;
    delete autoStart_;
    delete settings_;
}

QString AppController::connectionStatus() const { return connectionStatus_; }

QWidget *AppController::createMainWindow() {
    if (window_) return window_;
    window_ = new QMainWindow();
    window_->setWindowTitle(QStringLiteral("ClipBridge"));
    window_->resize(860, 640);
    window_->setMinimumSize(800, 620);

    auto *central = new QWidget(window_);
    central->setObjectName(QStringLiteral("central"));
    auto *layout = new QVBoxLayout(central);
    layout->setContentsMargins(28, 24, 28, 24);
    layout->setSpacing(16);

    auto *headerWidget = new WindowDragHeader(central);
    headerWidget->setObjectName(QStringLiteral("windowHeader"));
    auto *header = new QHBoxLayout(headerWidget);
    header->setContentsMargins(0, 0, 0, 0);
    header->setSpacing(12);
    auto *logo = new QLabel(QStringLiteral("C"), central);
    logo->setObjectName(QStringLiteral("logo"));
    logo->setAlignment(Qt::AlignCenter);
    logo->setFixedSize(46, 46);
    auto *titleColumn = new QVBoxLayout();
    titleColumn->setSpacing(1);
    auto *title = new QLabel(QStringLiteral("ClipBridge"), central);
    title->setObjectName(QStringLiteral("title"));
    QFont titleFont = title->font();
    titleFont.setPointSize(20);
    titleFont.setBold(true);
    title->setFont(titleFont);
    auto *subtitle =
        new QLabel(QStringLiteral("私人端到端加密剪贴板"), central);
    subtitle->setObjectName(QStringLiteral("subtitle"));
    logo->setAttribute(Qt::WA_TransparentForMouseEvents);
    title->setAttribute(Qt::WA_TransparentForMouseEvents);
    subtitle->setAttribute(Qt::WA_TransparentForMouseEvents);
    titleColumn->addWidget(title);
    titleColumn->addWidget(subtitle);
    header->addWidget(logo);
    header->addLayout(titleColumn);
    header->addStretch();
    statusLabel_ =
        new QLabel(QStringLiteral("●  %1").arg(connectionStatus_), central);
    statusLabel_->setObjectName(QStringLiteral("connectionStatus"));
    statusLabel_->setProperty("connected",
                              connectionStatus_ == QStringLiteral("已连接"));
    statusLabel_->setAttribute(Qt::WA_TransparentForMouseEvents);
    header->addWidget(statusLabel_);
    auto *hideButton = new QPushButton(QStringLiteral("隐藏"), central);
    hideButton->setObjectName(QStringLiteral("secondaryButton"));
    hideButton->setFixedWidth(72);
    header->addWidget(hideButton);
    connect(hideButton, &QPushButton::clicked, window_, &QWidget::hide);
    auto *closeButton = new QPushButton(QStringLiteral("退出"), central);
    closeButton->setObjectName(QStringLiteral("secondaryButton"));
    closeButton->setFixedWidth(72);
    header->addWidget(closeButton);
    connect(closeButton, &QPushButton::clicked, qApp, &QApplication::quit);
    layout->addWidget(headerWidget);

    syncCheck_ =
        new QCheckBox(QStringLiteral("启用剪贴板自动同步"), central);
    syncCheck_->setObjectName(QStringLiteral("syncSwitch"));
    syncCheck_->setChecked(settings_->syncEnabled());
    connect(syncCheck_, &QCheckBox::toggled, this,
            [this](bool enabled) { settings_->setSyncEnabled(enabled); });

    auto *usbCard = card(central);
    auto *usbCardLayout = new QHBoxLayout(usbCard);
    usbCardLayout->setContentsMargins(18, 15, 18, 15);
    usbCardLayout->setSpacing(14);
    auto *usbIcon = new QLabel(QStringLiteral("USB"), usbCard);
    usbIcon->setObjectName(QStringLiteral("smallIcon"));
    usbIcon->setAlignment(Qt::AlignCenter);
    usbIcon->setFixedSize(46, 38);
    auto *usbText = new QVBoxLayout();
    usbText->setSpacing(2);
    auto *usbTitle =
        new QLabel(QStringLiteral("手机重启后恢复后台互通"), usbCard);
    usbTitle->setObjectName(QStringLiteral("sectionTitle"));
    auto *usbStatus = new QLabel(androidUsb_->status(), usbCard);
    usbStatus->setObjectName(QStringLiteral("muted"));
    usbStatus->setWordWrap(true);
    usbText->addWidget(usbTitle);
    usbText->addWidget(usbStatus);
    auto *usbButton =
        new QPushButton(QStringLiteral("一键恢复"), usbCard);
    usbButton->setObjectName(QStringLiteral("secondaryButton"));
    usbCardLayout->addWidget(usbIcon);
    usbCardLayout->addLayout(usbText, 1);
    usbCardLayout->addWidget(usbButton);
    layout->addWidget(usbCard);
    connect(usbButton, &QPushButton::clicked, androidUsb_,
            &AndroidUsbConnector::activateClipBridge);
    connect(androidUsb_, &AndroidUsbConnector::statusChanged, usbStatus,
            &QLabel::setText);
    connect(androidUsb_, &AndroidUsbConnector::activationFinished, this,
            [this](bool success, const QString &message) {
                window_->statusBar()->showMessage(message, 7000);
                if (!success) {
                    QMessageBox::information(
                        window_, QStringLiteral("USB 恢复手机互通"), message);
                }
            });
    QTimer::singleShot(500, androidUsb_, &AndroidUsbConnector::refresh);

    auto *content = new QHBoxLayout();
    content->setSpacing(16);
    auto *devicesCard = card(central);
    auto *devicesLayout = new QVBoxLayout(devicesCard);
    devicesLayout->setContentsMargins(18, 16, 18, 16);
    devicesLayout->setSpacing(10);
    auto *devicesHeader = new QHBoxLayout();
    auto *devicesTitle = new QLabel(QStringLiteral("接收设备"), devicesCard);
    devicesTitle->setObjectName(QStringLiteral("sectionTitle"));
    devicesHeader->addWidget(devicesTitle);
    devicesHeader->addStretch();
    devicesHeader->addWidget(syncCheck_);
    devicesLayout->addLayout(devicesHeader);
    auto *devicesHint =
        new QLabel(QStringLiteral("复制内容会发送到选中的在线设备"), devicesCard);
    devicesHint->setObjectName(QStringLiteral("muted"));
    devicesLayout->addWidget(devicesHint);
    deviceList_ = new QListWidget(devicesCard);
    deviceList_->setSelectionMode(QAbstractItemView::ExtendedSelection);
    connect(deviceList_, &QListWidget::itemSelectionChanged, this, [this] {
        QStringList targets;
        for (QListWidgetItem *item : deviceList_->selectedItems()) {
            targets.push_back(item->data(Qt::UserRole).toString());
        }
        sync_->setTargetDeviceIds(targets);
        settings_->setTargetDeviceIds(targets);
    });
    devicesLayout->addWidget(deviceList_);

    auto *recentCard = card(central);
    auto *recentLayout = new QVBoxLayout(recentCard);
    recentLayout->setContentsMargins(18, 16, 18, 16);
    recentLayout->setSpacing(10);
    auto *recentTitle =
        new QLabel(QStringLiteral("最近收到的内容"), recentCard);
    recentTitle->setObjectName(QStringLiteral("sectionTitle"));
    auto *recentHint =
        new QLabel(QStringLiteral("内容只在本机解密和保存"), recentCard);
    recentHint->setObjectName(QStringLiteral("muted"));
    recentLayout->addWidget(recentTitle);
    recentLayout->addWidget(recentHint);
    recentText_ = new QTextEdit(recentCard);
    recentText_->setReadOnly(true);
    recentText_->setPlaceholderText(QStringLiteral("尚未收到剪贴板内容"));
    recentLayout->addWidget(recentText_);
    content->addWidget(devicesCard, 1);
    content->addWidget(recentCard, 1);
    layout->addLayout(content, 1);

    auto *actionsCard = card(central);
    auto *buttons = new QGridLayout(actionsCard);
    buttons->setContentsMargins(14, 14, 14, 14);
    buttons->setHorizontalSpacing(10);
    buttons->setVerticalSpacing(10);
    auto *sendButton =
        new QPushButton(QStringLiteral("发送当前剪贴板"), actionsCard);
    sendButton->setObjectName(QStringLiteral("primaryButton"));
    auto *connectButton =
        new QPushButton(QStringLiteral("连接 / 重连"), actionsCard);
    auto *pairButton = new QPushButton(QStringLiteral("配对手机"), actionsCard);
    auto *pauseButton =
        new QPushButton(QStringLiteral("暂停十分钟"), actionsCard);
    auto *historyButton =
        new QPushButton(QStringLiteral("历史记录"), actionsCard);
    auto *settingsButton = new QPushButton(QStringLiteral("设置"), actionsCard);
    buttons->addWidget(sendButton, 0, 0, 1, 2);
    buttons->addWidget(connectButton, 0, 2, 1, 2);
    buttons->addWidget(pairButton, 1, 0);
    buttons->addWidget(pauseButton, 1, 1);
    buttons->addWidget(historyButton, 1, 2);
    buttons->addWidget(settingsButton, 1, 3);
    layout->addWidget(actionsCard);
    connect(sendButton, &QPushButton::clicked, sync_,
            [this] { sync_->sendCurrentClipboard(); });
    connect(connectButton, &QPushButton::clicked, this, [this] {
        const TokenPair pair = tokens_->tokens();
        if (pair.isValid()) {
            websocket_->connectToServer(pair.accessToken, devices_->deviceId());
        } else {
            showLoginDialog();
        }
    });
    connect(pairButton, &QPushButton::clicked, this,
            &AppController::showPairingDialog);
    connect(pauseButton, &QPushButton::clicked, this, [this] {
        settings_->setPausedUntil(QDateTime::currentMSecsSinceEpoch() + 600000);
        window_->statusBar()->showMessage(QStringLiteral("同步已暂停十分钟"), 5000);
    });
    connect(historyButton, &QPushButton::clicked, this,
            &AppController::showHistoryDialog);
    connect(settingsButton, &QPushButton::clicked, this,
            &AppController::showSettingsDialog);
    auto *resizeRow = new QHBoxLayout();
    resizeRow->setContentsMargins(0, 0, 0, 0);
    resizeRow->addStretch();
    resizeRow->addWidget(new WindowResizeHandle(window_, central));
    layout->addLayout(resizeRow);
    window_->setCentralWidget(central);
    applyTheme();
    window_->statusBar()->showMessage(connectionStatus_);

    return window_;
}

void AppController::start() {
    if (!window_) createMainWindow();
    const TokenPair pair = tokens_->tokens();
    if (!pair.isValid()) {
        QTimer::singleShot(0, this, &AppController::showLoginDialog);
        return;
    }
    if (settings_->autoConnect()) {
        beginSession();
    } else {
        api_->fetchDevices(pair.accessToken);
        setConnectionStatus(QStringLiteral("自动连接已关闭"));
    }
}

void AppController::beginSession() {
    const TokenPair pair = tokens_->tokens();
    if (!pair.isValid()) return;
    tokenRefreshTimer_.start();
    api_->fetchDevices(pair.accessToken);
    websocket_->connectToServer(pair.accessToken, devices_->deviceId());
}

void AppController::showLoginDialog() {
    QDialog dialog(window_);
    dialog.setObjectName(QStringLiteral("loginDialog"));
    dialog.setWindowTitle(QStringLiteral("登录 ClipBridge"));
    dialog.setWindowFlags(dialog.windowFlags() | Qt::WindowStaysOnTopHint);
    dialog.setModal(true);
    auto *layout = new QFormLayout(&dialog);
    auto *email = new QLineEdit(&dialog);
    auto *password = new QLineEdit(&dialog);
    password->setEchoMode(QLineEdit::Password);
    layout->addRow(QStringLiteral("邮箱"), email);
    layout->addRow(QStringLiteral("密码"), password);
    auto *buttons = new QDialogButtonBox(&dialog);
    auto *login = buttons->addButton(QStringLiteral("登录"), QDialogButtonBox::AcceptRole);
    auto *registration =
        buttons->addButton(QStringLiteral("注册"), QDialogButtonBox::ActionRole);
    layout->addRow(buttons);
    connect(login, &QPushButton::clicked, this, [this, email, password] {
        api_->authenticate(email->text(), password->text(), false);
    });
    connect(registration, &QPushButton::clicked, this, [this, email, password] {
        api_->authenticate(email->text(), password->text(), true);
    });
    connect(api_, &ApiClient::requestFailed, &dialog,
            [&dialog](const QString &message) {
                QMessageBox::warning(&dialog, QStringLiteral("认证失败"), message);
            });
    dialog.exec();
}

void AppController::refreshDeviceList() {
    if (!deviceList_) return;
    QStringList eligibleIds;
    for (const Device &device : devices_->devices()) {
        if (device.id != devices_->deviceId() && !device.revoked) {
            eligibleIds.push_back(device.id);
        }
    }
    QStringList validTargets;
    const QStringList currentTargets = sync_->targetDeviceIds();
    for (const QString &id : currentTargets) {
        if (eligibleIds.contains(id)) validTargets.push_back(id);
    }
    if (validTargets.isEmpty() && eligibleIds.size() == 1) {
        validTargets = eligibleIds;
    }
    const QSet<QString> selected(validTargets.cbegin(), validTargets.cend());
    const QSignalBlocker blocker(deviceList_);
    deviceList_->clear();
    for (const Device &device : devices_->devices()) {
        if (device.id == devices_->deviceId() || device.revoked) continue;
        const QString label = QStringLiteral("%1 · %2 · %3")
                                  .arg(device.name, device.platform,
                                       device.online ? QStringLiteral("在线")
                                                     : QStringLiteral("离线"));
        auto *item = new QListWidgetItem(label, deviceList_);
        item->setData(Qt::UserRole, device.id);
        item->setSelected(selected.contains(device.id));
    }
    sync_->setTargetDeviceIds(validTargets);
    settings_->setTargetDeviceIds(validTargets);
}

void AppController::showHistoryDialog() {
    QDialog dialog(window_);
    dialog.setWindowTitle(QStringLiteral("剪贴板历史"));
    dialog.resize(850, 520);
    auto *layout = new QVBoxLayout(&dialog);
    auto *search = new QLineEdit(&dialog);
    search->setPlaceholderText(QStringLiteral("搜索历史"));
    auto *table = new QTableWidget(&dialog);
    table->setColumnCount(5);
    table->setHorizontalHeaderLabels({QStringLiteral("收藏"), QStringLiteral("内容"),
                                      QStringLiteral("来源"), QStringLiteral("时间"),
                                      QStringLiteral("状态")});
    table->horizontalHeader()->setSectionResizeMode(1, QHeaderView::Stretch);
    table->setSelectionBehavior(QAbstractItemView::SelectRows);
    table->setEditTriggers(QAbstractItemView::NoEditTriggers);
    auto reload = [this, table, search] {
        const QVector<HistoryRecord> records = history_->search(search->text());
        table->setRowCount(records.size());
        for (int row = 0; row < records.size(); ++row) {
            const HistoryRecord &record = records.at(row);
            auto *favorite = new QTableWidgetItem(record.favorite
                                                      ? QStringLiteral("★")
                                                      : QStringLiteral("☆"));
            favorite->setData(Qt::UserRole, record.id);
            favorite->setData(Qt::UserRole + 1, record.favorite);
            favorite->setData(Qt::UserRole + 2, record.content);
            table->setItem(row, 0, favorite);
            table->setItem(row, 1, new QTableWidgetItem(record.content.left(300)));
            table->setItem(row, 2, new QTableWidgetItem(record.sourceDevice));
            table->setItem(
                row, 3,
                new QTableWidgetItem(
                    QDateTime::fromMSecsSinceEpoch(record.createdAt).toString(
                        QStringLiteral("yyyy-MM-dd HH:mm:ss"))));
            table->setItem(row, 4,
                           new QTableWidgetItem(record.sent
                                                    ? QStringLiteral("已确认")
                                                    : QStringLiteral("等待确认")));
        }
    };
    connect(search, &QLineEdit::textChanged, &dialog,
            [reload](const QString &) { reload(); });
    layout->addWidget(search);
    layout->addWidget(table);
    auto *buttons = new QHBoxLayout();
    auto *copy = new QPushButton(QStringLiteral("复制"), &dialog);
    auto *favorite = new QPushButton(QStringLiteral("收藏/取消"), &dialog);
    auto *remove = new QPushButton(QStringLiteral("删除"), &dialog);
    auto *clear = new QPushButton(QStringLiteral("清空全部"), &dialog);
    buttons->addWidget(copy);
    buttons->addWidget(favorite);
    buttons->addWidget(remove);
    buttons->addStretch();
    buttons->addWidget(clear);
    layout->addLayout(buttons);
    connect(copy, &QPushButton::clicked, &dialog, [this, table] {
        if (table->currentRow() < 0) return;
        const QString text = table->item(table->currentRow(), 0)
                                 ->data(Qt::UserRole + 2)
                                 .toString();
        clipboard_->writeRemoteText(text, crypto_->contentHash(text));
    });
    connect(favorite, &QPushButton::clicked, &dialog, [this, table, reload] {
        if (table->currentRow() < 0) return;
        QTableWidgetItem *item = table->item(table->currentRow(), 0);
        history_->setFavorite(item->data(Qt::UserRole).toLongLong(),
                              !item->data(Qt::UserRole + 1).toBool());
        reload();
    });
    connect(remove, &QPushButton::clicked, &dialog, [this, table, reload] {
        if (table->currentRow() < 0) return;
        history_->remove(table->item(table->currentRow(), 0)
                             ->data(Qt::UserRole)
                             .toLongLong());
        reload();
    });
    connect(clear, &QPushButton::clicked, &dialog, [this, reload, &dialog] {
        if (QMessageBox::question(&dialog, QStringLiteral("清空历史"),
                                  QStringLiteral("确定清空所有历史记录？")) ==
            QMessageBox::Yes) {
            history_->clear();
            reload();
        }
    });
    reload();
    dialog.exec();
}

void AppController::showSettingsDialog() {
    QDialog dialog(window_);
    dialog.setWindowTitle(QStringLiteral("设置"));
    auto *layout = new QFormLayout(&dialog);
    auto *server = new QLineEdit(settings_->serverUrl().toString(), &dialog);
    auto *theme = new QComboBox(&dialog);
    theme->addItem(QStringLiteral("蓝色"), QStringLiteral("blue"));
    theme->addItem(QStringLiteral("绿色"), QStringLiteral("green"));
    theme->addItem(QStringLiteral("紫色"), QStringLiteral("purple"));
    theme->addItem(QStringLiteral("橙色"), QStringLiteral("orange"));
    theme->addItem(QStringLiteral("中性"), QStringLiteral("neutral"));
    theme->setCurrentIndex(
        qMax(0, theme->findData(settings_->themeColor())));
    auto *autoConnect = new QCheckBox(&dialog);
    autoConnect->setChecked(settings_->autoConnect());
    auto *autoStart = new QCheckBox(&dialog);
    autoStart->setChecked(autoStart_->isEnabled());
    auto *autoWrite = new QCheckBox(&dialog);
    autoWrite->setChecked(settings_->autoWriteRemote());
    auto *maxLength = new QSpinBox(&dialog);
    maxLength->setRange(100, 1000000);
    maxLength->setValue(settings_->maxTextLength());
    auto *retention = new QSpinBox(&dialog);
    retention->setRange(1, 3650);
    retention->setValue(settings_->retentionDays());
    auto *maxItems = new QSpinBox(&dialog);
    maxItems->setRange(10, 100000);
    maxItems->setValue(settings_->maxHistoryItems());
    auto *offline = new QComboBox(&dialog);
    offline->addItem(QStringLiteral("不保存"), 0);
    offline->addItem(QStringLiteral("1 分钟"), 60);
    offline->addItem(QStringLiteral("10 分钟"), 600);
    offline->addItem(QStringLiteral("1 小时"), 3600);
    offline->setCurrentIndex(qMax(0, offline->findData(settings_->offlineTtlSeconds())));
    layout->addRow(QStringLiteral("服务器地址"), server);
    layout->addRow(QStringLiteral("主题颜色"), theme);
    layout->addRow(QStringLiteral("自动连接"), autoConnect);
    layout->addRow(QStringLiteral("开机启动"), autoStart);
    layout->addRow(QStringLiteral("自动写入远端内容"), autoWrite);
    layout->addRow(QStringLiteral("最大文本字节数"), maxLength);
    layout->addRow(QStringLiteral("历史保留天数"), retention);
    layout->addRow(QStringLiteral("最大历史条数"), maxItems);
    layout->addRow(QStringLiteral("离线消息有效期"), offline);
    auto *buttons =
        new QDialogButtonBox(QDialogButtonBox::Save | QDialogButtonBox::Cancel,
                             &dialog);
    auto *logoutButton =
        buttons->addButton(QStringLiteral("退出登录"), QDialogButtonBox::DestructiveRole);
    layout->addRow(buttons);
    connect(buttons, &QDialogButtonBox::accepted, &dialog, [&] {
        const QUrl url(server->text().trimmed());
        if (!url.isValid() || url.scheme() != QStringLiteral("https")) {
            QMessageBox::warning(&dialog, QStringLiteral("地址无效"),
                                 QStringLiteral("服务器地址必须是有效的 HTTPS 地址"));
            return;
        }
        settings_->setServerUrl(url);
        settings_->setThemeColor(theme->currentData().toString());
        settings_->setAutoConnect(autoConnect->isChecked());
        settings_->setAutoWriteRemote(autoWrite->isChecked());
        settings_->setMaxTextLength(maxLength->value());
        settings_->setRetentionDays(retention->value());
        settings_->setMaxHistoryItems(maxItems->value());
        settings_->setOfflineTtlSeconds(offline->currentData().toInt());
        if (!autoStart_->setEnabled(autoStart->isChecked())) {
            QMessageBox::warning(&dialog, QStringLiteral("开机启动"),
                                 QStringLiteral("更新开机启动设置失败"));
        }
        history_->cleanup(retention->value(), maxItems->value());
        applyTheme();
        dialog.accept();
    });
    connect(buttons, &QDialogButtonBox::rejected, &dialog, &QDialog::reject);
    connect(logoutButton, &QPushButton::clicked, &dialog, [this, &dialog] {
        dialog.accept();
        logout();
    });
    dialog.exec();
}

void AppController::showPairingDialog() {
    auto *dialog = new QDialog(window_);
    dialog->setAttribute(Qt::WA_DeleteOnClose);
    dialog->setWindowTitle(QStringLiteral("配对 Android 手机"));
    auto *layout = new QVBoxLayout(dialog);
    auto *description = new QLabel(
        QStringLiteral("正在创建五分钟有效的一次性配对二维码…"), dialog);
    description->setWordWrap(true);
    auto *image = new QLabel(dialog);
    image->setAlignment(Qt::AlignCenter);
    layout->addWidget(description);
    layout->addWidget(image);
    auto *close =
        new QDialogButtonBox(QDialogButtonBox::Close, Qt::Horizontal, dialog);
    connect(close, &QDialogButtonBox::rejected, dialog, &QDialog::close);
    layout->addWidget(close);
    connect(pairing_, &PairingManager::qrReady, dialog,
            [description, image](const QImage &qr, const QString &, qint64 expires) {
                image->setPixmap(QPixmap::fromImage(qr));
                description->setText(
                    QStringLiteral("使用 Android ClipBridge 扫描。二维码将于 %1 过期，"
                                   "不包含密码、私钥或长期令牌。")
                        .arg(QDateTime::fromMSecsSinceEpoch(expires).toString(
                            QStringLiteral("HH:mm:ss"))));
            });
    connect(pairing_, &PairingManager::failed, dialog,
            [description](const QString &message) {
                description->setText(QStringLiteral("配对失败：") + message);
            });
    dialog->show();
    pairing_->create(tokens_->tokens().accessToken);
}

void AppController::logout() {
    const TokenPair pair = tokens_->tokens();
    websocket_->disconnectFromServer();
    tokenRefreshTimer_.stop();
    api_->logout(pair.accessToken, pair.refreshToken);
    tokens_->clear();
    if (notifications_) notifications_->setConnected(false);
    setConnectionStatus(QStringLiteral("已退出登录"));
    showLoginDialog();
}

void AppController::applyTheme() {
    if (!window_) return;
    const bool dark =
        QApplication::palette().color(QPalette::Window).lightness() < 128;
    const QString accent = themeAccent(settings_->themeColor(), dark);
    const QString background =
        dark ? QStringLiteral("#111318") : QStringLiteral("#F6F7F9");
    const QString cardColor =
        dark ? QStringLiteral("#1B1D23") : QStringLiteral("#FFFFFF");
    const QString inputColor =
        dark ? QStringLiteral("#15171C") : QStringLiteral("#FAFBFC");
    const QString text =
        dark ? QStringLiteral("#F0F1F5") : QStringLiteral("#202226");
    const QString muted =
        dark ? QStringLiteral("#AEB2BD") : QStringLiteral("#676B74");
    const QString border =
        dark ? QStringLiteral("#353841") : QStringLiteral("#DDE0E5");
    const QString hover =
        dark ? QStringLiteral("#272A32") : QStringLiteral("#EEF0F3");
    const QString selectionText = QStringLiteral("#FFFFFF");

    QString style = QStringLiteral(R"(
        QMainWindow, QWidget#central {
            background: $BACKGROUND;
            color: $TEXT;
        }
        QLabel#logo {
            background: $ACCENT;
            color: white;
            border-radius: 10px;
            font-size: 20px;
            font-weight: 700;
        }
        QLabel#title { color: $TEXT; }
        QLabel#subtitle, QLabel#muted {
            color: $MUTED;
            font-size: 12px;
        }
        QLabel#sectionTitle {
            color: $TEXT;
            font-size: 15px;
            font-weight: 600;
        }
        QLabel#smallIcon {
            background: $HOVER;
            color: $ACCENT;
            border-radius: 8px;
            font-size: 11px;
            font-weight: 700;
        }
        QLabel#connectionStatus {
            color: $MUTED;
            padding: 8px 12px;
            border: 1px solid $BORDER;
            border-radius: 8px;
        }
        QLabel#connectionStatus[connected="true"] {
            color: #4F916B;
        }
        QFrame#card {
            background: $CARD;
            border: 1px solid $BORDER;
            border-radius: 12px;
        }
        QPushButton {
            min-height: 38px;
            padding: 0 15px;
            color: $TEXT;
            background: $INPUT;
            border: 1px solid $BORDER;
            border-radius: 8px;
            font-weight: 500;
        }
        QPushButton:hover { background: $HOVER; }
        QPushButton:pressed { border-color: $ACCENT; }
        QPushButton:disabled { color: $MUTED; }
        QPushButton#primaryButton {
            color: white;
            background: $ACCENT;
            border-color: $ACCENT;
            font-weight: 600;
        }
        QPushButton#primaryButton:hover { background: $ACCENT; }
        QPushButton#secondaryButton {
            color: $ACCENT;
            border-color: $ACCENT;
            background: transparent;
        }
        QListWidget, QTextEdit, QLineEdit, QComboBox, QSpinBox, QTableWidget {
            color: $TEXT;
            background: $INPUT;
            border: 1px solid $BORDER;
            border-radius: 8px;
            padding: 7px;
            selection-background-color: $ACCENT;
            selection-color: $SELECTION_TEXT;
        }
        QListWidget::item {
            min-height: 34px;
            padding: 5px 8px;
            border-radius: 6px;
        }
        QListWidget::item:hover { background: $HOVER; }
        QListWidget::item:selected {
            color: $SELECTION_TEXT;
            background: $ACCENT;
        }
        QCheckBox { color: $TEXT; spacing: 8px; }
        QStatusBar {
            color: $MUTED;
            background: $BACKGROUND;
            border-top: 1px solid $BORDER;
        }
        QDialog {
            color: $TEXT;
            background: $BACKGROUND;
        }
        QHeaderView::section {
            color: $TEXT;
            background: $HOVER;
            border: none;
            border-bottom: 1px solid $BORDER;
            padding: 8px;
        }
    )");
    style.replace(QStringLiteral("$BACKGROUND"), background);
    style.replace(QStringLiteral("$CARD"), cardColor);
    style.replace(QStringLiteral("$INPUT"), inputColor);
    style.replace(QStringLiteral("$TEXT"), text);
    style.replace(QStringLiteral("$MUTED"), muted);
    style.replace(QStringLiteral("$BORDER"), border);
    style.replace(QStringLiteral("$HOVER"), hover);
    style.replace(QStringLiteral("$ACCENT"), accent);
    style.replace(QStringLiteral("$SELECTION_TEXT"), selectionText);
    window_->setStyleSheet(style);
}

void AppController::setConnectionStatus(const QString &status) {
    if (connectionStatus_ == status) return;
    connectionStatus_ = status;
    if (statusLabel_) {
        statusLabel_->setText(QStringLiteral("●  %1").arg(status));
        statusLabel_->setProperty("connected", status == QStringLiteral("已连接"));
        statusLabel_->style()->unpolish(statusLabel_);
        statusLabel_->style()->polish(statusLabel_);
    }
    if (window_) window_->statusBar()->showMessage(status);
    if (notifications_ && status != QStringLiteral("已连接")) {
        notifications_->setConnected(false);
    }
    emit connectionStatusChanged();
}
