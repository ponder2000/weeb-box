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
    downloadVideo(animeNames[intrest], epNum, link)


if __name__ == "__main__":
    main()
