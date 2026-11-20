@echo off
REM factor-lab PRODUCTION launcher.
REM Data source: Tushare Pro (real A-share data).
setlocal enabledelayedexpansion
cd /d "%~dp0"

if "%TUSHARE_INDEX%"=="" set TUSHARE_INDEX=000300.SH
if "%TUSHARE_START%"=="" set TUSHARE_START=20200101
if "%TUSHARE_TOPN%"=="" set TUSHARE_TOPN=0

if "%TUSHARE_TOKEN%"=="" (
  echo.
  echo [NOTE] TUSHARE_TOKEN is not set.
  echo   Token can also be saved in the UI: 设置 - Tushare Token
  echo   or: set TUSHARE_TOKEN=your_tushare_token
  echo   Get a token at https://tushare.pro/user/token
  echo.
)

if not exist factor-lab.exe (
  echo [1/2] building backend...
  go build -o factor-lab.exe ./cmd/server
  if errorlevel 1 ( echo build failed & pause & exit /b 1 )
)

if not exist web\dist\index.html (
  echo [2/2] building frontend...
  pushd web
  call npm install
  call npm run build
  popd
)

echo.
echo factor-lab PRODUCTION  http://127.0.0.1:8080
echo   data source : Tushare Pro  (real A-share)
echo   index       : %TUSHARE_INDEX%    topN: %TUSHARE_TOPN%    start: %TUSHARE_START%
echo   NOTE: first load can take several minutes depending on Tushare rate limits.
if not "%FACTOR_LAB_TOKEN%"=="" echo   auth        : token required (browser will ask once)
echo.
start "" http://127.0.0.1:8080
REM Token is read from TUSHARE_TOKEN. If FACTOR_LAB_TOKEN is set, the UI asks for it once.
factor-lab.exe -addr 127.0.0.1:8080 ^
  -data tushare -index %TUSHARE_INDEX% -topn %TUSHARE_TOPN% -start %TUSHARE_START% ^
  -store data\factors.json -web web\dist -api-token "%FACTOR_LAB_TOKEN%"
pause



cd web
npm run build
cd ..
go run ./cmd/server -addr 127.0.0.1:8080 -data tushare -web web/dist