#include "NotificationManager.h"

#include <QAction>
#include <QApplication>
#include <QMenu>
#include <QStyle>
#include <QSystemTrayIcon>
#include <QWidget>

NotificationManager::NotificationManager(QWidget *window, QObject *parent)
    : QObject(parent), tray_(new QSystemTrayIcon(this)) {
    tray_->setIcon(QApplication::style()->standardIcon(QStyle::SP_DialogApplyButton));
    tray_->setToolTip(QStringLiteral("ClipBridge"));
    auto *menu = new QMenu(window);
    statusAction_ = menu->addAction(QStringLiteral("状态：未连接"));
    statusAction_->setEnabled(false);
    menu->addSeparator();
    connect(menu->addAction(QStringLiteral("开启/关闭同步")), &QAction::triggered,
            this, &NotificationManager::toggleSyncRequested);
    connect(menu->addAction(QStringLiteral("暂停十分钟")), &QAction::triggered,
            this, &NotificationManager::pauseRequested);
    connect(menu->addAction(QStringLiteral("发送当前剪贴板")), &QAction::triggered,
            this, &NotificationManager::sendRequested);
    connect(menu->addAction(QStringLiteral("历史记录")), &QAction::triggered,
            this, &NotificationManager::historyRequested);
    connect(menu->addAction(QStringLiteral("打开主窗口")), &QAction::triggered,
            this, &NotificationManager::showRequested);
    menu->addSeparator();
    connect(menu->addAction(QStringLiteral("退出")), &QAction::triggered,
            this, &NotificationManager::quitRequested);
    tray_->setContextMenu(menu);
    connect(tray_, &QSystemTrayIcon::activated, this,
            [this](QSystemTrayIcon::ActivationReason reason) {
                // GNOME 的 AppIndicator 通常不会产生 DoubleClick，只会上报
                // Trigger。两种事件都响应，避免托盘图标看起来“点不动”。
                if (reason == QSystemTrayIcon::Trigger ||
                    reason == QSystemTrayIcon::DoubleClick) {
                    emit showRequested();
                }
            });
    tray_->show();
}

void NotificationManager::setConnected(bool connected) {
    statusAction_->setText(connected ? QStringLiteral("状态：已连接")
                                     : QStringLiteral("状态：未连接"));
}

void NotificationManager::showClipboardMessage(const QString &source,
                                                const QString &preview,
                                                bool sensitive) {
    const QString body = sensitive ? QStringLiteral("检测到疑似敏感内容，正文已隐藏")
                                   : preview.left(160);
    tray_->showMessage(QStringLiteral("来自 %1").arg(source), body,
                       QSystemTrayIcon::Information, 8000);
}
