import os
import subprocess
import sys


def main() -> int:
    binary = os.path.join(os.path.dirname(__file__), "bin", "actionbox")
    if os.name == "nt" and not os.path.exists(binary):
        binary += ".exe"
    return subprocess.call([binary, *sys.argv[1:]])


if __name__ == "__main__":
    sys.exit(main())