from model import GogoAnime
from media_helper import streamVideo, downloadVideo


def main():
    anime = GogoAnime()
    q = input("Enter the search querry : ")
    animeNames, animeLinks = anime.search(q)
    intrest = 0
    for i, name in enumerate(animeNames):
        print(f"{i} || {name}")
    intrest = int(input("Choose a valid number : "))
    epNum = int(input("Enter episode Num : "))
    link = anime.getLink(animeNames[intrest], animeLinks[intrest], epNum)
    mode = input("Want to stream or download? (s/d) :")
    if mode == 's':
        streamVideo(animeNames[intrest], epNum, link)
    elif mode == 'd':
        downloadVideo(animeNames[intrest], epNum, link)
    else:
        print("Invalid choice!!")


if __name__ == "__main__":
    main()
    print("End Program")
