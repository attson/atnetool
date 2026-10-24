@echo off
setlocal
set "PID=%~1"
set "SOURCE=%~2"
set "DESTINATION=%~3"
set "ARCHIVE=%~4"
set "STAGE=%~5"
set "LOG=%LOCALAPPDATA%\ATNetool\update-%PID%.log"
if not exist "%LOCALAPPDATA%\ATNetool" mkdir "%LOCALAPPDATA%\ATNetool"

for /L %%I in (1,1,120) do (
  tasklist /FI "PID eq %PID%" 2>NUL | find "%PID%" >NUL || goto replace
  timeout /t 1 /nobreak >NUL
)
echo Application did not exit before timeout.>>"%LOG%"
exit /b 1

:replace
set "BACKUP=%DESTINATION%.old"
if exist "%BACKUP%" del /f /q "%BACKUP%"
move /y "%DESTINATION%" "%BACKUP%" >>"%LOG%" 2>&1
if errorlevel 1 exit /b 1
copy /y "%SOURCE%" "%DESTINATION%" >>"%LOG%" 2>&1
if errorlevel 1 (
  move /y "%BACKUP%" "%DESTINATION%" >>"%LOG%" 2>&1
  exit /b 1
)
del /f /q "%BACKUP%"
start "" "%DESTINATION%"
del /f /q "%ARCHIVE%"
rmdir /s /q "%STAGE%"
endlocal
(goto) 2>NUL & del /f /q "%~f0"
