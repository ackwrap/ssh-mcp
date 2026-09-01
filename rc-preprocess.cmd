@echo off
set "input="
:next
if "%~1"=="" goto read
set "input=%~1"
shift
goto next
:read
type "%input%"
