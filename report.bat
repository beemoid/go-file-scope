@echo off
REM File Report Collector - Windows Batch Script
REM Scans C:\screen-capturer and all subdirectories, sends to Go API server

setlocal enabledelayedexpansion

REM Configuration
set SERVER_URL=http://localhost:5555/command
set SCAN_PATH=C:\screen-capturer
set TEMP_FILE=%TEMP%\file_report_%RANDOM%.json

REM Get system information - use IP as hostname
for /f "tokens=*" %%A in ('ipconfig ^| findstr /R "IPv4"') do (
    set IP_LINE=%%A
    for /f "tokens=15" %%B in ("!IP_LINE!") do set HOST_IP=%%B
)

if "!HOST_IP!"=="" set HOST_IP=127.0.0.1

REM Get current timestamp in ISO format
for /f "tokens=2-4 delims=/ " %%a in ('date /t') do (set MYDATE=%%c-%%a-%%b)
for /f "tokens=1-2 delims=/:" %%a in ('time /t') do (set MYTIME=%%a:%%b)
set TIMESTAMP=!MYDATE!T!MYTIME!:00Z

echo.
echo ╔════════════════════════════════════════╗
echo ║   File Report Collector Started         ║
echo ╚════════════════════════════════════════╝
echo.
echo Host IP: !HOST_IP!
echo Scan Path: !SCAN_PATH!
echo Timestamp: !TIMESTAMP!
echo.

REM Check if scan path exists
if not exist "!SCAN_PATH!" (
    echo Error: !SCAN_PATH! does not exist
    pause
    exit /b 1
)

REM Create JSON file with header
(
    echo {
    echo   "host_ip": "!HOST_IP!",
    echo   "timestamp": "!TIMESTAMP!",
    echo   "base_path": "!SCAN_PATH!",
    echo   "directories": [
) > "!TEMP_FILE!"

REM Initialize directory counter
set DIR_COUNT=0

REM Scan all subdirectories recursively
for /d /r "!SCAN_PATH!" %%D in (*) do (
    set FOLDER=%%D
    
    REM Count files in this folder (non-recursive, only direct children)
    for /f %%A in ('dir "!FOLDER!" /b 2^>nul ^| find /c /v ""') do set FILE_COUNT=%%A
    if "!FILE_COUNT!"=="" set FILE_COUNT=0
    
    REM Get folder size using dir /s
    set FOLDER_SIZE=0
    for /f "tokens=3" %%A in ('dir "!FOLDER!" /s 2^>nul ^| find "bytes"') do (
        set FOLDER_SIZE=%%A
    )
    if "!FOLDER_SIZE!"=="" set FOLDER_SIZE=0
    
    REM Convert bytes to MB
    if !FOLDER_SIZE! gtr 0 (
        set /a FOLDER_SIZE_MB=!FOLDER_SIZE! / 1048576
    ) else (
        set FOLDER_SIZE_MB=0
    )
    
    REM Add comma if not first item
    if !DIR_COUNT! gtr 0 (
        echo     }, >> "!TEMP_FILE!"
    )
    
    REM Add directory entry to JSON
    (
        echo     {
        echo       "path": "!FOLDER!",
        echo       "file_count": !FILE_COUNT!,
        echo       "size_bytes": !FOLDER_SIZE!,
        echo       "size_mb": !FOLDER_SIZE_MB!
    ) >> "!TEMP_FILE!"
    
    echo Scanned: !FOLDER! - !FILE_COUNT! files, !FOLDER_SIZE_MB! MB
    
    set /a DIR_COUNT=!DIR_COUNT! + 1
)

REM Close last directory entry
(
    echo     }
    echo   ],
    echo   "total_directories": !DIR_COUNT!
    echo }
) >> "!TEMP_FILE!"

echo.
echo Total directories scanned: !DIR_COUNT!
echo.
echo Sending report to server at !SERVER_URL!
echo.

REM Send to server
curl -X POST "!SERVER_URL!" ^
  -H "Content-Type: application/json" ^
  -d @"!TEMP_FILE!" 2>nul

if !ERRORLEVEL! equ 0 (
    echo ✓ Report sent successfully!
) else (
    echo ✗ Error sending report. Check if server is running.
)

REM Cleanup
if exist "!TEMP_FILE!" del "!TEMP_FILE!"

echo.
pause
