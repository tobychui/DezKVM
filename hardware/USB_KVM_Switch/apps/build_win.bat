@echo off
set CGO_CXXFLAGS=-IC:/PROGRA~2/WI3CF2~1/10/Include/100261~1.0/winrt
go build -ldflags="-H=windowsgui" %*
