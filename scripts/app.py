from model import GogoAnime
from media_helper import streamVideo, downloadVideo
import helper as hlp


def main():
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


if __name__ == "__main__":
    main()
    hlp.printf("Program executed successfully", col='green')
