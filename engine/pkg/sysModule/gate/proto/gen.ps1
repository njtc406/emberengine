
param (
    [string]$ProtoPath = ".",
    [string]$OutputPath = ".",
    [string[]]$ProtoFiles
)

# 检查 protoc 是否安装
try {
    $protocVersion = & protoc --version
    Write-Host "Using protoc version: $protocVersion"
} catch {
    Write-Error "protoc is not installed or not in PATH. Please install Protocol Buffers compiler first."
    exit 1
}

# 检查 Go protobuf 插件是否安装
try {
    $protocGenGoPath = (Get-Command protoc-gen-go).Source
    Write-Host "Found protoc-gen-go at: $protocGenGoPath"
} catch {
    Write-Error "protoc-gen-go is not installed. Please install it with: go install google.golang.org/protobuf/cmd/protoc-gen-go@latest"
    exit 1
}

try {
    $protocGenGrpcPath = (Get-Command protoc-gen-go-grpc).Source
    Write-Host "Found protoc-gen-go-grpc at: $protocGenGrpcPath"
} catch {
    Write-Error "protoc-gen-go-grpc is not installed. Please install it with: go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest"
    exit 1
}

# 设置完整路径
$ProtoPath = Resolve-Path $ProtoPath
$OutputPath = Resolve-Path $OutputPath

# 如果没有指定具体文件，则获取目录下所有 .proto 文件
if (-not $ProtoFiles) {
    $ProtoFiles = Get-ChildItem -Path $ProtoPath -Filter *.proto | Select-Object -ExpandProperty Name
    if (-not $ProtoFiles) {
        Write-Error "No .proto files found in $ProtoPath"
        exit 1
    }
}

# 为每个 .proto 文件执行编译
foreach ($file in $ProtoFiles) {
    $protoFile = Join-Path -Path $ProtoPath -ChildPath $file

    if (-not (Test-Path $protoFile)) {
        Write-Warning "File not found: $protoFile"
        continue
    }

    Write-Host "Compiling $file to Go..."

    # 执行 protoc 命令
    & protoc --proto_path=$ProtoPath --go_out=$OutputPath --go-grpc_out=$OutputPath --go_opt=paths=source_relative $protoFile

    if ($LASTEXITCODE -ne 0) {
        Write-Error "Failed to compile $file"
    } else {
        Write-Host "Successfully compiled $file"
    }
}

Write-Host "Proto compilation completed."