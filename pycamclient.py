import signal
import socket
import subprocess
import threading
import time

HOST = "0.0.0.0"
PORT = 9999
WIDTH = 640
HEIGHT = 480
FRAMERATE = 60
BITRATE = 4000000
INTRA = 30
RESTART_DELAY = 1.0
SEND_TIMEOUT = 2.0
CHUNK_SIZE = 65536

COMMAND = [
    "rpicam-vid",
    "-t", "0",
    "--mode", f"{WIDTH}:{HEIGHT}",
    "--width", str(WIDTH),
    "--height", str(HEIGHT),
    "--framerate", str(FRAMERATE),
    "--codec", "h264",
    "--bitrate", str(BITRATE),
    "--intra", str(INTRA),
    "--inline",
    "--flush",
    "--nopreview",
    "-o", "-",
]

stopping = threading.Event()
client_lock = threading.Lock()
client = None
process = None


def set_client(new_client):
    """Installs a new viewer, replacing (and closing) any previous one."""
    global client
    with client_lock:
        old, client = client, new_client
    if old is not None:
        old.close()


def accept_loop(server):
    while not stopping.is_set():
        try:
            conn, addr = server.accept()
        except OSError:
            break
        conn.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
        conn.settimeout(SEND_TIMEOUT)
        print(f"viewer connected: {addr[0]}:{addr[1]}", flush=True)
        set_client(conn)


def broadcast(data):
    """Sends data to the current viewer; a dead or slow viewer is dropped, never the camera."""
    global client
    with client_lock:
        conn = client
    if conn is None:
        return
    try:
        conn.sendall(data)
    except OSError:
        print("viewer disconnected", flush=True)
        with client_lock:
            if client is conn:
                client = None
        conn.close()


def stream_camera():
    """Runs rpicam-vid and forwards its output; restarts it only if it dies."""
    global process
    while not stopping.is_set():
        process = subprocess.Popen(COMMAND, stdout=subprocess.PIPE, bufsize=0)
        while True:
            data = process.stdout.read(CHUNK_SIZE)
            if not data:
                break
            broadcast(data)
        process.wait()
        if stopping.is_set():
            break
        print(f"rpicam-vid exited with code {process.returncode}. Restarting...", flush=True)
        time.sleep(RESTART_DELAY)


def handle_signal(signum, frame):
    stopping.set()
    if process is not None and process.poll() is None:
        process.terminate()


def main():
    signal.signal(signal.SIGTERM, handle_signal)
    signal.signal(signal.SIGINT, handle_signal)

    server = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    server.bind((HOST, PORT))
    server.listen(1)
    threading.Thread(target=accept_loop, args=(server,), daemon=True).start()

    try:
        stream_camera()
    finally:
        server.close()
        set_client(None)
        if process is not None and process.poll() is None:
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()


if __name__ == "__main__":
    main()
