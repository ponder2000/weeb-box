import requests
import numpy as np
import cv2
from tqdm import tqdm


def downloadVideo(animeName: str, animeEpisode: int, downloadableLink: str):
    _fileName = f"../downloads/{animeName}-{animeEpisode}.mp4"
    respose = requests.get(downloadableLink, stream=True)
    if respose.status_code == 200:
        print("Download has been started....")
        print(f"file name : {_fileName}")
        with open(_fileName, 'wb') as f:
            for chunk in tqdm(respose.iter_content(chunk_size=1024)):
                if chunk:
                    f.write(chunk)
                    f.flush()
        print("Download completed 😁")
    else:
        print(
            f"Received unexpected status code {response.status_code} while downloading")


def streamVideo(animeName: str, animeEpisode: ImportWarning, downloadableLink: str):
    windowName = f"{animeName}-{animeEpisode}"
    vcap = cv2.VideoCapture(downloadableLink)
    cv2.namedWindow(windowName, cv2.WINDOW_NORMAL)
    cv2.resizeWindow(windowName, 720, 520)
    while True:
        ret, frame = vcap.read()
        if frame is not None:
            cv2.imshow(windowName, frame)
            if cv2.waitKey(22) & 0xFF == ord('q'):
                break
        else:
            print("Frame is none")
            break

    vcap.release()
    cv2.destroyAllWindows()
