@echo off
setlocal
pushd "%~dp0"
"%~dp0output\denova.exe" %*
set "denova_exit_code=%errorlevel%"
popd
if not "%denova_exit_code%"=="0" pause
exit /b %denova_exit_code%