import requests
from bs4 import BeautifulSoup
import os


class GogoAnime:
    def __init__(self):
        self.websiteName = "gogoanime"
        self.baseUrl = "https://gogoanime.sh/"

    # helper function for search
    def _search(self, link: str):
        _animeNames = []
        _animeLinks = []
        response = requests.get(link)
        if response.status_code == 200:
            soup = BeautifulSoup(response.text, features="html.parser")
            results = soup.find_all("li")
            for li in results:
                _name = li.find("p", class_="name")
                if _name:
                    # extracting name of anime and link from the search query
                    _animeNames.append(_name.a['title'])
                    _animeLinks.append(_name.a['href'])
            return _animeNames, _animeLinks
        else:
            print(
                f"Received unexpected status code {response.status_code} while using helper search function")

    # searching anime by keywords
    def search(self, query: str):
        _link = self.baseUrl + f"/search.html?keyword={query}"
        animeNames, animeLinks = self._search(_link)
        return animeNames, animeLinks

    # get link of page of a perticular specified episode
    def _getEpisodeLink(self, animeName: str, animeLink: str, episodeNum: int):
        animeLink = animeLink.split('/')[-1]
        episodePageLink = self.baseUrl + animeLink + f"-episode-{episodeNum}"
        return episodePageLink

    # get link for the downloadPage of the specific episode
    def _getDownloadPageLink(self, episodeLink):
        r = requests.get(episodeLink)
        if r.status_code == 200:
            soup = BeautifulSoup(r.text, features="html.parser")
            tags = soup.find_all('li')
            for li in tags:
                try:
                    classes = li['class']
                    for c in classes:
                        if c == "dowloads":
                            return li.a['href']
                except:
                    # li tags with no classes
                    pass
            return "-1"
        else:
            print(
                f"Received unexpected status code {r.status_code} while getting download-page link")

    # get the link for the downlaod of the anime
    def _getDownloadableLink(self, linkToDownloadPage):
        r = requests.get(linkToDownloadPage)
        if r.status_code == 200:
            soup = BeautifulSoup(r.text, features="html.parser")
            tags = soup.find_all('a')
            for a in tags:
                try:
                    a['download']
                    return a['href']
                except:
                    # a with no download attribute
                    pass
            return "-1"
        else:
            print(
                f"Received unexpected status code {r.status_code} while getting download link")

    # max number of available episodes still not working
    def getMaxEpisode(self, animeLink: str):
        r = requests.get(self.baseUrl + animeLink)
        if r.status_code == 200:
            soup = BeautifulSoup(r.text, features="html.parser")
            tags = soup.find_all('li')
            raise NotImplementedError
            pass
            return "-1"
        else:
            print(
                f"Received unexpected status code {r.status_code} while getting download-page link")

    def getLink(self, animeName, animeLink, episodeNum):
        episodeLink = self._getEpisodeLink(animeName, animeLink, episodeNum)
        downloadPage = self._getDownloadPageLink(episodeLink)
        link = self._getDownloadableLink(downloadPage)
        return link
