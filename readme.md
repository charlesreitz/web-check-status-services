# Serviço de monitoramento via porta 


# Build Windows para linux
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -o seu_programa_linux