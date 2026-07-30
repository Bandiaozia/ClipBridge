#!/usr/bin/env wish

# 端到端诊断：读取 X11 CLIPBOARD，而不是 PRIMARY 选择区。
wm withdraw .
update
if {[catch {clipboard get} value]} {
    puts stderr $value
    exit 1
}
puts -nonewline $value
exit
