import os
from model import GogoAnime
from media_helper import streamVideo, downloadVideo
import helper as hlp


currEpisode = 1


def saveLastDownloadLog(path):
    global currEpisode
    with open(f'{path}/lastFile.txt', 'w') as fp:
        fp.write(str(currEpisode - 1))
    print("log file saved..")


def downloadAll(anime, animeNames, animeLinks, intrest):
    global currEpisode
    choice = input("Want to start download from first episode? (y/n) : ")
    if choice == 'n':
        try:
            # trying to fetch last completely downloaded episode
            path = hlp.getDownlaodPath()
            path = os.path.join(path, animeNames[intrest])
            fp = open(f'{path}/lastFile.txt', 'r')
            currEpisode = int(fp.read())
            currEpisode += 1
        except:
            # else ask for the episode number
            currEpisode = int(
                input("Enter episode number from where you want to download : "))
    try:
        path = hlp.getDownlaodPath()
        os.mkdir(os.path.join(path, animeNames[intrest]))
    except Exception as e:
        hlp.printf(f"Error in while creating dir : {e}", col='red')

    while True:
        try:
            link = anime.getLink(
                animeNames[intrest], animeLinks[intrest], currEpisode)
        except Exception as e:
            hlp.printf(
                f"Error in getting link for episode = {currEpisode}\nPossible reasons : This episode is end of all the available episodes", col='red')
            break
        try:
            downloadVideo(animeNames[intrest], currEpisode,
                          link, subDir=animeNames[intrest])
        except Exception as e:
            hlp.printf(f"Error while downloading whole anime : {e}", col='red')
            return
        currEpisode += 1


def downloadEpisodes(anime, animeNames, animeLinks, intrest):
    epNum = int(input("Enter episode Num : "))
    try:
        link = anime.getLink(animeNames[intrest], animeLinks[intrest], epNum)
    except Exception as error:
        hlp.printf(error, col='red')
        exit()

    mode = input("Want to stream or download? (s/d) :")
    if mode == 's':
        streamVideo(animeNames[intrest], epNum, link)
    elif mode == 'd':
        downloadVideo(animeNames[intrest], epNum, link)
    else:
        hlp.printf("Invalid choice!!", col='red')


def main():
    global currEpisode
    anime = GogoAnime()
    q = input("Enter the search querry : ")
    animeNames, animeLinks = anime.search(q)
    for i, name in enumerate(animeNames):
        print(f"{i} || {name}")
    intrest = int(input("Choose a valid index : "))
    if intrest >= len(animeNames):
        hlp.printf(f"This index doesn't exist", col='cyan')
        hlp.printf("Exiting code !!", col='red')
        exit()
    choice = int(input(
        "Enter your choice :\n1. Download whole anime\n2. Download certain episode\nyour choice : "))
    if choice == 2:
        downloadEpisodes(anime, animeNames, animeLinks, intrest)
    elif choice == 1:
        try:
            downloadAll(anime, animeNames, animeLinks, intrest)
        except Exception as e:
            hlp.printf(e, col='red')
            print("saving logs..")
        finally:
            path = hlp.getDownlaodPath()
            path = os.path.join(path, animeNames[intrest])
            saveLastDownloadLog(path)
    else:
        hlp.printf("Bad choice !!", col='cyan')


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt as e:
        hlp.printf(e, col='red')
    hlp.printf("Program executed successfully", col='green')
