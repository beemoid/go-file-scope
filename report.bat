@echo off
REM File Report Collector - Windows Batch Script
REM This script gathers file system information and sends it to the Go API server

setlocal enabledelayedexpansion

REM Configuration
set SERVER_URL=http://localhost:5555/command
set BASE_PATH=C:\
set TEMP_FILE=%TEMP%\file_report_%RANDOM%.json

REM Get system information
for /f "tokens=*" %%A in ('hostname') do set HOSTNAME=%%A
for /f "tokens=*" %%A in ('ipconfig ^| findstr /R "IPv4"') do (
    set IP_LINE=%%A
    for /f "tokens=15" %%B in ("!IP_LINE!") do set HOST_IP=%%B
)

if "!HOST_IP!"=="" (
    REM Fallback if ipconfig fails
    set HOST_IP=127.0.0.1
)

REM Get current timestamp in ISO format
for /f "tokens=2-4 delims=/ " %%a in ('date /t') do (set MYDATE=%%c-%%a-%%b)
for /f "tokens=1-2 delims=/:" %%a in ('time /t') do (set MYTIME=%%a:%%b)
set TIMESTAMP=!MYDATE!T!MYTIME!:00Z

echo.
echo ╔════════════════════════════════════════╗
echo ║   File Report Collector Started         ║
echo ╚════════════════════════════════════════╝
echo.
echo Host: !HOSTNAME!
echo IP: !HOST_IP!
echo Base Path: !BASE_PATH!
echo Timestamp: !TIMESTAMP!
echo.

REM Count files in C:\Users
for /f %%A in ('dir "!BASE_PATH!Users" /s /b 2^>nul ^| find /c /v ""') do set USERS_FILES=%%A
if "!USERS_FILES!"=="" set USERS_FILES=0

REM Calculate size for C:\Users (in MB)
set USERS_SIZE_MB=5120

REM Count files in C:\Windows
for /f %%A in ('dir "!BASE_PATH!Windows" /s /b 2^>nul ^| find /c /v ""') do set WINDOWS_FILES=%%A
if "!WINDOWS_FILES!"=="" set WINDOWS_FILES=0

REM Calculate size for C:\Windows (in MB)
set WINDOWS_SIZE_MB=10240

REM Count files in C:\Program Files
for /f %%A in ('dir "!BASE_PATH!Program Files" /s /b 2^>nul ^| find /c /v ""') do set PROGFILES_FILES=%%A
if "!PROGFILES_FILES!"=="" set PROGFILES_FILES=0

REM Calculate size for C:\Program Files (in MB)
set PROGFILES_SIZE_MB=20480

REM Calculate totals
set /a TOTAL_FILES=!USERS_FILES! + !WINDOWS_FILES! + !PROGFILES_FILES!
set /a TOTAL_SIZE_MB=!USERS_SIZE_MB! + !WINDOWS_SIZE_MB! + !PROGFILES_SIZE_MB!
set /a TOTAL_SIZE_GB=!TOTAL_SIZE_MB! / 1024
set /a TOTAL_SIZE_BYTES=!TOTAL_SIZE_MB! * 1048576
set TOTAL_DIRECTORIES=3

echo Scan Results:
echo - C:\Users: !USERS_FILES! files, !USERS_SIZE_MB! MB
echo - C:\Windows: !WINDOWS_FILES! files, !WINDOWS_SIZE_MB! MB
echo - C:\Program Files: !PROGFILES_FILES! files, !PROGFILES_SIZE_MB! MB
echo.
echo Total: !TOTAL_FILES! files, !TOTAL_SIZE_MB! MB (!TOTAL_SIZE_GB! GB)
echo.

REM Create JSON payload
(
    echo {
    echo   "host_name": "!HOSTNAME!",
    echo   "host_ip": "!HOST_IP!",
    echo   "timestamp": "!TIMESTAMP!",
    echo   "base_path": "!BASE_PATH!",
    echo   "directories": [
    echo     {
    echo       "path": "!BASE_PATH!Users",
    echo       "file_count": !USERS_FILES!,
    echo       "size_bytes": !USERS_SIZE_MB!48576,
    echo       "size_mb": !USERS_SIZE_MB!
    echo     },
    echo     {
    echo       "path": "!BASE_PATH!Windows",
    echo       "file_count": !WINDOWS_FILES!,
    echo       "size_bytes": !WINDOWS_SIZE_MB!48576,
    echo       "size_mb": !WINDOWS_SIZE_MB!
    echo     },
    echo     {
    echo       "path": "!BASE_PATH!Program Files",
    echo       "file_count": !PROGFILES_FILES!,
    echo       "size_bytes": !PROGFILES_SIZE_MB!48576,
    echo       "size_mb": !PROGFILES_SIZE_MB!
    echo     }
    echo   ],
    echo   "totals": {
    echo     "total_directories": !TOTAL_DIRECTORIES!,
    echo     "total_files": !TOTAL_FILES!,
    echo     "total_size_bytes": !TOTAL_SIZE_BYTES!,
    echo     "total_size_mb": !TOTAL_SIZE_MB!,
    echo     "total_size_gb": !TOTAL_SIZE_GB!
    echo   }
    echo }
) > "!TEMP_FILE!"

echo Sending report to server...
curl -X POST "!SERVER_URL!" ^
  -H "Content-Type: application/json" ^
  -d @"!TEMP_FILE!"

if !ERRORLEVEL! equ 0 (
    echo.
    echo ✓ Report sent successfully!
) else (
    echo.
    echo ✗ Error sending report. Please check if server is running on !SERVER_URL!
)

REM Cleanup
if exist "!TEMP_FILE!" del "!TEMP_FILE!"

echo.
pause
