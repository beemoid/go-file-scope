@echo off
setlocal enabledelayedexpansion

REM ==================================================
REM ENTRY POINT
REM ==================================================
goto :MAIN


REM ==================================================
REM LOGGER FUNCTION
REM ==================================================
:log
REM usage: call :log LEVEL MESSAGE
echo [%date% %time%] %~1 %~2
echo [%date% %time%] %~1 %~2>>"%LOG_FILE%"
exit /b


REM ==================================================
REM MAIN
REM ==================================================
:MAIN

REM ================= CONFIG =================
set SERVER_URL=http://192.168.211.1:5555/command
set SCAN_PATH=C:\Users\User 46\Documents\New folder\screen-capturer

REM ==== CURL STAGING FOLDER (CHANGE LATER) ====
set CURL_DIR=C:\ss-check\

REM ==== CLEANUP OPTIONS ====
set DELETE_CURL_AFTER_RUN=1
set DELETE_LOGS_AFTER_RUN=0

REM ================= SCRIPT DIR =================
set SCRIPT_DIR=%~dp0

REM ================= ENSURE CURL DIR =================
if not exist "%CURL_DIR%" mkdir "%CURL_DIR%" 2>nul

REM ================= OUTPUT FILES (ALL IN CURL DIR) =================
set LOG_FILE=%CURL_DIR%\report.log
set JSON_FILE=%CURL_DIR%\file_report.json
set DIR_LIST=%CURL_DIR%\dir_list.lst
set CURL_OUTPUT=%CURL_DIR%\curl_output.out

REM ================= TEST MODE =================
set TEST_MODE=0
if /i "%1"=="test" set TEST_MODE=1

REM ================= START =================
echo ========================================>>"%LOG_FILE%"
call :log INFO "Script started"
echo ========================================>>"%LOG_FILE%"

REM ================= VALIDATION =================
if not exist "%SCAN_PATH%" (
    call :log ERROR "Scan path not found: %SCAN_PATH%"
    goto :CLEANUP
)

REM ================= STAGE CURL =================
if not exist "%SCRIPT_DIR%curl.exe" (
    call :log ERROR "curl.exe not found in script directory"
    goto :CLEANUP
)

move /y "%SCRIPT_DIR%curl.exe" "%CURL_DIR%\curl.exe" >nul
if errorlevel 1 (
    call :log ERROR "Failed to move curl.exe"
    goto :CLEANUP
)

set CURL_EXE=%CURL_DIR%\curl.exe
call :log INFO "curl staged at %CURL_EXE%"

REM ================= SYSTEM INFO =================
set HOST_NAME=%COMPUTERNAME%
set HOST_IP=127.0.0.1

for /f "tokens=2 delims=:" %%A in ('ipconfig ^| findstr "IPv4"') do (
    set HOST_IP=%%A
    set HOST_IP=!HOST_IP:~1!
    goto :IP_DONE
)
:IP_DONE

call :log INFO "Host=%HOST_NAME% IP=%HOST_IP%"

REM ================= CLEAN OLD OUTPUTS =================
for %%F in ("%JSON_FILE%" "%DIR_LIST%" "%CURL_OUTPUT%") do (
    if exist %%F del %%F 2>nul
)

REM ================= JSON HEADER =================
set "JSON_BASE_PATH=%SCAN_PATH:\=/%"

(
echo {
echo   "host_name": "%HOST_NAME%",
echo   "host_ip": "%HOST_IP%",
echo   "base_path": "%JSON_BASE_PATH%",
echo   "directories": [
)> "%JSON_FILE%"

REM ================= SCAN =================
echo %SCAN_PATH%>"%DIR_LIST%"
dir /b /s /a:d "%SCAN_PATH%" >>"%DIR_LIST%" 2>nul

set DIR_COUNT=0
set TOTAL_FILES=0
set TOTAL_SIZE=0

for /f "usebackq delims=" %%D in ("%DIR_LIST%") do (
    for /f %%C in ('dir "%%D" /a:-d /b 2^>nul ^| find /c /v ""') do set FILES=%%C

    set SIZE=0
    for /f "tokens=3" %%S in ('dir "%%D" /a:-d /-c 2^>nul ^| findstr "File(s)"') do (
        set SIZE=%%S
        set SIZE=!SIZE:,=!
    )

    set /a SIZE_MB=!SIZE!/1048576
    set /a TOTAL_FILES+=!FILES!
    set /a TOTAL_SIZE+=!SIZE!

    if !DIR_COUNT! gtr 0 echo     ,>>"%JSON_FILE%"

    set "P=%%D"
    set "P=!P:\=/!"

    (
    echo     {
    echo       "path": "!P!",
    echo       "file_count": !FILES!,
    echo       "size_bytes": !SIZE!,
    echo       "size_mb": !SIZE_MB!
    echo     }
    )>>"%JSON_FILE%"

    call :log INFO "Dir=%%D Files=!FILES! SizeMB=!SIZE_MB!"
    set /a DIR_COUNT+=1
)

REM ================= JSON FOOTER =================
set /a TOTAL_MB=!TOTAL_SIZE!/1048576
set /a TOTAL_GB=!TOTAL_SIZE!/1073741824

(
echo   ],
echo   "totals": {
echo     "directories": !DIR_COUNT!,
echo     "files": !TOTAL_FILES!,
echo     "size_bytes": !TOTAL_SIZE!,
echo     "size_mb": !TOTAL_MB!,
echo     "size_gb": !TOTAL_GB!
echo   }
echo }
)>>"%JSON_FILE%"

call :log SUCCESS "Scan completed"

REM ================= SEND =================
"%CURL_EXE%" -X POST "%SERVER_URL%" ^
 -H "Content-Type: application/json" ^
 -d @"%JSON_FILE%" >"%CURL_OUTPUT%" 2>&1

if errorlevel 1 (
    call :log ERROR "Upload failed"
) else (
    call :log SUCCESS "Upload successful"
)

REM ================= CLEANUP =================
:CLEANUP

if %DELETE_CURL_AFTER_RUN%==1 (
    if exist "%CURL_EXE%" (
        del "%CURL_EXE%" 2>nul
        call :log INFO "curl.exe deleted"
    )
)

if %DELETE_LOGS_AFTER_RUN%==1 (
    del "%LOG_FILE%" "%JSON_FILE%" "%DIR_LIST%" "%CURL_OUTPUT%" 2>nul
)

call :log INFO "Script finished"
echo.
pause