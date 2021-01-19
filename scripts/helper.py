import os
import details


PINK_COLOR = '\033[95m'
RED_COLOR = '\033[91m'
GREEN_COLOR = '\033[92m'
OKCYAN = '\033[96m'
COLOR_END = '\033[0m'


def getDownlaodPath():
    if details.downloadPath != None:
        return details.downloadPath

    dirName = "Downloads"
    downloadPath = os.path.join(os.getcwd(), dirName)
    try:
        os.mkdir(downloadPath)
        print(f"New directory created : {downloadPath}")
    except OSError as error:
        print(f"Will be downloaded to : {downloadPath}")
    return downloadPath


def printf(msg: str, col: str):
    if col == 'red':
        print(f"{RED_COLOR}{msg}{COLOR_END}")
    elif col == 'pink':
        print(f"{PINK_COLOR}{msg}{COLOR_END}")
    elif col == 'green':
        print(f"{GREEN_COLOR}{msg}{COLOR_END}")
    elif col == 'cyan':
        print(f"{OKCYAN}{msg}{COLOR_END}")
    else:
        print(msg)
