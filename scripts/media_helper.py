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


# TODO : This function ain't working fine

def streamVideo(animeName: str, animeEpisode: ImportWarning, downloadableLink: str):
    # link = "https://gogo-play.net/goto.php?url=aHR0cHM6LyAawehyfcghysfdsDGDYdgdsfsdfwstdgdsgtert9zdG9yYWdlLmdvb2dsZWFwaXMuY29tLzI5MjIxNS9ROUZVTk9NX1VKTkkvMjJhXzE2MDkxNzQyMjgxNDkwMjEubXA0"
    _fileName = f"{animeName}-{animeEpisode}.mp4"
    respose = requests.get(downloadableLink, stream=True)
    if respose.status_code == 200:
        bts = bytes()
        for chunk in respose.iter_content(chunk_size=1024):
            bts += chunk
            a = bts.find(b'\xff\xd8')
            b = bts.find(b'\xff\xd9')
            if a != -1 and b != -1:
                jpg = bts[a:b+2]
                bts = bts[b+2:]
                i = cv2.imdecode(np.frombuffer(
                    jpg, dtype=np.uint8), cv2.IMREAD_COLOR)
                cv2.imshow(_fileName, i)
                if cv2.waitKey(1) == 27:
                    exit(0)
    else:
        print(
            f"Received unexpected status code {response.status_code} (while streaming the video)")
