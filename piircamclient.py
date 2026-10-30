import subprocess
import time

def start_rpicam_vid():
    """Starts the rpicam-vid process and returns the subprocess object."""
    command = "rpicam-vid -t 0 --width 640 --height 480 --framerate 50 --inline --listen -o tcp://0.0.0.0:9999"
    process = subprocess.Popen(command, shell=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    return process

def monitor_process(process):
    """Monitors the process and restarts it if it dies."""
    while True:
        if process.poll() is not None:
            print("rpicam-vid process died. Restarting...")
            process = start_rpicam_vid()
        time.sleep(1)  # Check every second

def main():
    process = start_rpicam_vid()
    monitor_process(process)

if __name__ == "__main__":
    main()