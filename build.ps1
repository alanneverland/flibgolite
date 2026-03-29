param (
    [switch]$Docker # Флаг: собирать ли локальный Docker-образ
)

$ErrorActionPreference = "Stop"

# --- 1. Подготовка ---
if (!(Test-Path "./go.mod")) {
    Write-Host "Error: go.mod not found!" -ForegroundColor Red
    return
}

if (!(Test-Path "./dist")) { New-Item -ItemType Directory -Path "./dist" | Out-Null }

# Если зависимости уже скачаны, эту строку можно закомментировать для работы вообще без интернета
Write-Host "Checking local dependencies..." -ForegroundColor Yellow
go mod tidy 

# Жестко задаем версию вручную (никакого Git)
$VERSION = "2.0.0-al"

# --- 2. Сборка бинарников ---
$source = "./cmd/flibgolite"

# Флаги с передачей нашей локальной версии внутрь бинарника
$flags = "-s -w -X 'main.version=$VERSION'"

# Массив всех платформ и архитектур
$targets = @(
    @{ OS="linux"; Arch="amd64"; Arm="" },
    @{ OS="linux"; Arch="386"; Arm="" },
    @{ OS="linux"; Arch="arm64"; Arm="" },
    @{ OS="linux"; Arch="arm"; Arm="6" },
    @{ OS="linux"; Arch="arm"; Arm="7" },
    
    @{ OS="windows"; Arch="amd64"; Arm="" },
    @{ OS="windows"; Arch="386"; Arm="" },
    @{ OS="windows"; Arch="arm64"; Arm="" },
    
    @{ OS="darwin"; Arch="amd64"; Arm="" },
    @{ OS="darwin"; Arch="arm64"; Arm="" },
    
    @{ OS="freebsd"; Arch="amd64"; Arm="" },
    @{ OS="freebsd"; Arch="386"; Arm="" },
    @{ OS="freebsd"; Arch="arm64"; Arm="" },
    @{ OS="freebsd"; Arch="arm"; Arm="6" },
    @{ OS="freebsd"; Arch="arm"; Arm="7" }
)

Write-Host "`n--- Starting Local Cross-Platform Build ---" -ForegroundColor Cyan

foreach ($t in $targets) {
    $env:GOOS = $t.OS
    $env:GOARCH = $t.Arch
    if ($t.Arm) { 
        $env:GOARM = $t.Arm 
    } else { 
        Remove-Item Env:\GOARM -ErrorAction SilentlyContinue 
    }

    $outName = "flibgolite-al-$($t.OS)-$($t.Arch)"
    if ($t.Arm) { $outName += "v$($t.Arm)" }
    if ($t.OS -eq "windows") { $outName += ".exe" }

    $suffix = if ($t.Arm) { "v" + $t.Arm } else { "" }
    Write-Host "Building $($t.OS) $($t.Arch)$suffix..."
    
    # Сборка
    go build -ldflags $flags -o "./dist/$outName" $source
}

# Очищаем переменные окружения
Remove-Item Env:\GOOS -ErrorAction SilentlyContinue
Remove-Item Env:\GOARCH -ErrorAction SilentlyContinue
Remove-Item Env:\GOARM -ErrorAction SilentlyContinue

Write-Host "`nBinaries successfully finished! Check 'dist' folder." -ForegroundColor Green

# --- 3. Сборка локального Docker-образа ---
if ($Docker) {
    Write-Host "`n--- Building Local Docker Images ---" -ForegroundColor Cyan
    
    $APP = "flibgolite"
    
    Write-Host "Building Docker image for vinser/${APP}:${VERSION}..."
    
    # Собираем образы и оставляем их только в локальном кэше Docker
    docker buildx build --platform linux/amd64,linux/arm64,linux/arm/v7,linux/arm/v6 --tag "vinser/${APP}:${VERSION}" .
    docker image tag "vinser/${APP}:${VERSION}" "vinser/${APP}:latest"

    Write-Host "`nDocker images successfully built and stored locally." -ForegroundColor Green
}