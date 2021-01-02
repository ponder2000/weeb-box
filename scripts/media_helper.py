import requests
import numpy as np
import cv2
from tqdm import tqdm
import details
import helper as hlp


def downloadVideo(animeName: str, animeEpisode: int, downloadableLink: str):
    if details.downloadPath == None:
        _fileName = hlp.getDownlaodPath()
    else:
        _fileName = details.downloadPath
    _fileName += f"/{animeName}-{animeEpisode}.mp4"
    respose = requests.get(downloadableLink, stream=True)
    if respose.status_code == 200:
        hlp.printf("Download has been started....", col='pink')
        print(f"location : {_fileName}")
        with open(_fileName, 'wb') as f:
            for chunk in tqdm(respose.iter_content(chunk_size=1024)):
                if chunk:
                    f.write(chunk)
                    f.flush()
        hlp.printf("Download completed 😁", col='green')
    else:
        hlp.printf(
            f"Received unexpected status code {response.status_code} while downloading", col='cyan')
        exit()


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
