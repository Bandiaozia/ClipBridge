#pragma once

#include <QObject>

class QAction;
class QMenu;
class QSystemTrayIcon;
class QWidget;

class NotificationManager final : public QObject {
    Q_OBJECT

public:
    explicit NotificationManager(QWidget *window, QObject *parent = nullptr);
    void setConnected(bool connected);
    void showClipboardMessage(const QString &source, const QString &preview,
                              bool sensitive);

signals:
    void toggleSyncRequested();
    void pauseRequested();
    void sendRequested();
    void historyRequested();
    void showRequested();
    void quitRequested();

private:
    QSystemTrayIcon *tray_;
    QAction *statusAction_;
};

