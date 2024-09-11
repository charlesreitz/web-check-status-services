#!/bin/bash

# Nome do binário do seu programa
BINARY="web-check-status-services"
# Diretório onde o binário está localizado
BINARY_PATH="/caminho/para/o/seu/binario"
# Caminho para armazenar o arquivo PID
PID_FILE="$BINARY_PATH/web-check-status-services.pid"
# Arquivo de log
LOG_FILE="$BINARY_PATH/web-check-status-services.log"

# Função para iniciar o serviço
start() {
    if [ -f "$PID_FILE" ]; then
        PID=$(cat "$PID_FILE")
        if ps -p $PID > /dev/null; then
            echo "O serviço já está em execução (PID: $PID)."
            exit 1
        else
            echo "Arquivo PID existe, mas o processo não está rodando. Limpando o PID."
            rm "$PID_FILE"
        fi
    fi

    echo "Iniciando o serviço..."
    nohup "$BINARY_PATH/$BINARY" > "$LOG_FILE" 2>&1 &
    echo $! > "$PID_FILE"
    echo "Serviço iniciado com PID $(cat $PID_FILE)."
}

# Função para parar o serviço
stop() {
    if [ -f "$PID_FILE" ]; then
        PID=$(cat "$PID_FILE")
        echo "Parando o serviço com PID $PID..."
        kill $PID
        rm "$PID_FILE"
        echo "Serviço parado."
    else
        echo "Serviço não está em execução."
    fi
}

# Função para mostrar o status do serviço
status() {
    if [ -f "$PID_FILE" ]; then
        PID=$(cat "$PID_FILE")
        if ps -p $PID > /dev/null; then
            echo "O serviço está em execução (PID: $PID)."
        else
            echo "Arquivo PID existe, mas o processo não está rodando."
        fi
    else
        echo "Serviço não está em execução."
    fi
}

# Função para exibir o uso correto
usage() {
    echo "Uso: $0 {start|stop|status}"
    exit 1
}

# Checa os argumentos passados para o script
case "$1" in
    start)
        start
        ;;
    stop)
        stop
        ;;
    status)
        status
        ;;
    *)
        usage
        ;;
esac
