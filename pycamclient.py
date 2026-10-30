import signal
import subprocess
import sys
import time

PORT = 9999
WIDTH = 640
HEIGHT = 480
FRAMERATE = 60
BITRATE = 4000000
INTRA = 30
RESTART_DELAY = 1.0

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
    "--listen",
    "-o", f"tcp://0.0.0.0:{PORT}",
]

process = None
stopping = False


def start_rpicam_vid():
    """Starts the rpicam-vid process and returns the subprocess object."""
    return subprocess.Popen(COMMAND, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


def handle_signal(signum, frame):
    global stopping
    stopping = True
    if process is not None and process.poll() is None:
        process.terminate()


def monitor_process():
    """Restarts rpicam-vid whenever it exits (it exits after each viewer disconnects)."""
    global process
    while not stopping:
        process = start_rpicam_vid()
        process.wait()
        if stopping:
            break
        print("rpicam-vid exited. Restarting...", flush=True)
        time.sleep(RESTART_DELAY)


def main():
    signal.signal(signal.SIGTERM, handle_signal)
    signal.signal(signal.SIGINT, handle_signal)
    monitor_process()
    if process is not None and process.poll() is None:
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
    sys.exit(0)


if __name__ == "__main__":
    main()
