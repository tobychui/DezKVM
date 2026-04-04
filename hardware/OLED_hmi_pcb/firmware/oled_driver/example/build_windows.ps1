# Build script for Windows (PowerShell)

Write-Host "Building for Windows (amd64)..." -ForegroundColor Cyan
go build -v -o oled_sysinfo.exe

if ($LASTEXITCODE -eq 0) {
    Write-Host "`nBuild successful! Executable: oled_sysinfo.exe" -ForegroundColor Green
    Write-Host "`nUsage:" -ForegroundColor Yellow
    Write-Host "  .\oled_sysinfo.exe -port COM3"
} else {
    Write-Host "`nBuild failed!" -ForegroundColor Red
    exit 1
}
