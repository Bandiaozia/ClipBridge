#!/usr/bin/env wish

# 仅用于端到端测试：在 X11 上持有剪贴板六十秒，让运行中的桌面客户端
# 捕获变化，并留出时间验证远端消息不会反向覆盖本机内容。
if {$argc != 1} {
    puts stderr "usage: set-test-clipboard.tcl TEXT"
    exit 2
}
wm withdraw .
clipboard clear
clipboard append -- [lindex $argv 0]
update
after 60000 exit
