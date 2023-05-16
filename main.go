package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"

	// "fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sort"
	"strconv"

	// "strings"
	"time"

	"github.com/gin-gonic/gin"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/go-redis/redis/v8"
)

type Movie struct {
	Title string	`json:"title"`	
    EpisodeId uint	`json:"episode_id"`
    OpeningCrawl string	`json:"opening_crawl"`
    Director string	`json:"director"`
	Producer string	`json:"producer"`
	ReleaseDate string	`json:"release_date"`
	Characters[] string	`json:"characters"`
	Planets[] string	`json:"planets"`
	Starships[] string	`json:"starships"`
	Vehicles[] string	`json:"vehicles"`
	Species[] string	`json:"species"`
	Created string	`json:"created"`
    Edited string	`json:"edited"`
	Url string	`json:"url"`
}

type Character struct {
	Name string	`json:"name"`	
    Height string	`json:"height"`
    Mass string	`json:"mass"`
    HairColor string	`json:"hair_color"`
	SkinColor string	`json:"skin_color"`
	EyeColor string	`json:"eye_color"`
	BirthYear string	`json:"birth_year"`
	Gender string	`json:"gender"`
}

type CharacterData struct {
	Metadata CharacterMetaData	`json:"metadata"`	
    Characters []Character	`json:"character"`
}

type CharacterMetaData struct {
	TotalNumber int	`json:"total_number"`	
    TotalHeightCM string	`json:"total_height_cm"`
    TotalHeightFT string	`json:"total_height_ft"`
}

type RespBody struct {
	Count uint	`json:"count"`
    Next uint	`json:"next"`
    Previous uint	`json:"previous"`
    Results[] Movie	`json:"results"`
}

type MovieData struct {
	Name string	`json:"name"`	
    OpeningCrawl string	`json:"opening_crawl"`
    CommentCount uint	`json:"comment_count"`
}

type CommentBody struct {
	Comment string	`json:"comment"`
}

type Comment struct {
	gorm.Model

	MovieID uint `json:"movie_id"`
	Comment string	`json:"comment"`	
    IpAddress string	`json:"ip_address"`
}

type CommentData struct {
	Comment string	`json:"comment"`	
    IpAddress string	`json:"ip_address"`
	CreatedAt string `json:"created_at"`
}

var db *gorm.DB
var cache *redis.Client
var err error

func main() {
  r := gin.Default()

  http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true} 

  dsn := "host=postgres user=postgres password=postgres1234 dbname=postgres port=5432 sslmode=disable"
  db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})

  if err != nil {
	log.Fatalf(err.Error())
    panic("failed to connect database")
  }

  db.AutoMigrate(&Comment{})

  cache = redis.NewClient(
	&redis.Options{
		Addr: "redis:6379",
	})


  r.GET("/ping", func(c *gin.Context) {
    c.JSON(http.StatusOK, gin.H{
      "message": "pong",
    })
  })

  r.GET("/movies", GetMovies)
  r.GET("/movies/:id/comments", GetComments)
  r.POST("/movies/:id/comments", AddComment)
  r.GET("/movies/:id/characters", GetCharacters)

  r.Run() 
}

func GetMovies(ctx *gin.Context) {
	cacheData, err := cache.Get(ctx, "get-movies").Bytes()
    if err == nil {
		var data []MovieData

		err := json.Unmarshal(cacheData, &data)
		if err != nil {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			log.Fatalln("Error: ", err)
			return
		} 

		ctx.JSON(http.StatusOK, gin.H{
			"status": "success",
			"message": "movies retrieved successfully",
			"data": data,
		})

        return
    }

	resp, err := http.Get("https://swapi.dev/api/films")
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		log.Fatalln("Error: ", err)
		return
	}

	defer resp.Body.Close()

	respBody, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		ctx.JSON(resp.StatusCode, gin.H{"error": err.Error()})
		return
	}

	var result RespBody

	err = json.Unmarshal(respBody, &result)
    if err != nil {
        ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
    }

	movies := result.Results
	sort.SliceStable(movies, func(i, j int) bool {
		return movies[i].ReleaseDate > movies[j].ReleaseDate
	})

	var data []MovieData
	for _, movie := range movies {
		var commentCount int64
		db.Model(&Comment{}).Where("movie_id = ?", movie.EpisodeId).Count(&commentCount)
		movieData := MovieData {
			Name: movie.Title,
			OpeningCrawl: movie.OpeningCrawl,
			CommentCount: uint(commentCount),
		}

		data = append(data, movieData)
	}

	dataByte, err := json.Marshal(data)
    if err != nil {
        ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
    }

	cacheErr := cache.Set(ctx, "get-movies", dataByte, 3600*time.Second).Err()
    if cacheErr != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": cacheErr.Error()})
		return
    }

	ctx.JSON(http.StatusOK, gin.H{
		"status": "success",
		"message": "movies retrieved successfully",
		"data": data,
	})
}

func AddComment(ctx *gin.Context) {
	movieIdStr := ctx.Param("id")
	movieId, err :=  strconv.ParseUint(movieIdStr, 10, 64)

	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	defer ctx.Request.Body.Close()

	respBody, err := ioutil.ReadAll(ctx.Request.Body)

	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var commentBody CommentBody
	err = json.Unmarshal(respBody, &commentBody)
    if err != nil {
        ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
    }

	comment := commentBody.Comment

	if len(comment) > 500 {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "comment must be less than 500 characters"})
		return
	}

	ipAddress := ctx.ClientIP()


	commentData := Comment {
		MovieID: uint(movieId),
		Comment: comment,
		IpAddress: ipAddress,
	}

	result := db.Create(&commentData)

	if result.Error != nil {
        ctx.JSON(http.StatusInternalServerError, gin.H{"error": result.Error})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status": "success",
		"message": "comment inserted successfully",
		"data": commentData.ID,
	})
}

func GetComments(ctx *gin.Context) {
	movieIdStr := ctx.Param("id")
	movieId, err :=  strconv.ParseUint(movieIdStr, 10, 64)

	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var comments []Comment
	result := db.Where("movie_id = ?", uint(movieId)).Order("created_at desc").Find(&comments)

	if result.Error != nil {
        ctx.JSON(http.StatusInternalServerError, gin.H{"error": result.Error})
		return
	}

	var data []CommentData
	for _, comment := range comments {
		commentData := CommentData {
			Comment: comment.Comment,
			IpAddress: comment.IpAddress,
			CreatedAt: comment.CreatedAt.String(),
		}
		data = append(data, commentData)
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status": "success",
		"message": "comments retrived successfully",
		"data": data,
	})
}

func GetCharacters(ctx *gin.Context) {
	movieIdStr := ctx.Param("id")
	movieId, err :=  strconv.ParseUint(movieIdStr, 10, 64)

	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	sortParam := ctx.Query("sort")

	sortOrderParam := ctx.Query("asc")
	
	filterParam := ctx.Query("filter")

	resp, err := http.Get("https://swapi.dev/api/films")
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		log.Fatalln("Error: ", err)
		return
	}

	defer resp.Body.Close()

	respBody, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		ctx.JSON(resp.StatusCode, gin.H{"error": err.Error()})
		return
	}

	var result RespBody

	err = json.Unmarshal(respBody, &result)
    if err != nil {
        ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
    }

	movies := result.Results
	sort.SliceStable(movies, func(i, j int) bool {
		return movies[i].ReleaseDate > movies[j].ReleaseDate
	})

	var characterUrls []string
	for _, movie := range movies {
		if (movieId == uint64(movie.EpisodeId)) {
			characterUrls = movie.Characters
			break
		}
	}

	var characters []Character
	var totalHeight uint64 = 0

	for _, characterUrl := range characterUrls {
		resp, err := http.Get(characterUrl)
		if err != nil {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			log.Fatalln("Error: ", err)
			return
		}

		defer resp.Body.Close()

		respBody, err := ioutil.ReadAll(resp.Body)

		if err != nil {
			ctx.JSON(resp.StatusCode, gin.H{"error": err.Error()})
			return
		}

		var character Character

		err = json.Unmarshal(respBody, &character)
		if err != nil {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		h, _ := strconv.ParseUint(character.Height, 10, 64)
		totalHeight += h

		characters = append(characters, character)
	}

	if sortParam == "name" {
		if sortOrderParam == "true" {
			sort.SliceStable(characters, func(i, j int) bool {
				return characters[i].Name < characters[j].Name
			})
		}

		if sortOrderParam == "false" {
			sort.SliceStable(characters, func(i, j int) bool {
				return characters[i].Name > characters[j].Name
			})
		}
	}

	if sortParam == "gender" {
		if sortOrderParam == "true" {
			sort.SliceStable(characters, func(i, j int) bool {
				return characters[i].Gender < characters[j].Gender
			})
		}

		if sortOrderParam == "false" {
			sort.SliceStable(characters, func(i, j int) bool {
				return characters[i].Gender > characters[j].Gender
			})
		}
	}

	if sortParam == "height" {
		if sortOrderParam == "true" {
			sort.SliceStable(characters, func(i, j int) bool {
				h1, _ := strconv.ParseUint(characters[i].Height, 10, 64)
				h2, _ := strconv.ParseUint(characters[j].Height, 10, 64)
				return  h1 < h2
			})
		}

		if sortOrderParam == "false" {
			sort.SliceStable(characters, func(i, j int) bool {
				h1, _ := strconv.ParseUint(characters[i].Height, 10, 64)
				h2, _ := strconv.ParseUint(characters[j].Height, 10, 64)
				return  h1 > h2
			})
		}
	}


	var filteredCharacters []Character
	var data CharacterData

	if filterParam == "m" {
		var totalHeight uint64 = 0

		for _, character := range characters {
			if (character.Gender == "male") {
				filteredCharacters = append(filteredCharacters, character)

				h, _ := strconv.ParseUint(character.Height, 10, 64)
				totalHeight += h
			}
		}

		ftIn := float64(totalHeight) / 30.48
		ft := int64(ftIn)
		in := (ftIn - float64(int64(ft))) * 12

		metadata := CharacterMetaData {
			TotalNumber: len(filteredCharacters),
			TotalHeightCM: fmt.Sprintf("%dcm", totalHeight),
			TotalHeightFT: fmt.Sprintf("%dft and ", ft) + fmt.Sprintf("%.2f", in) + "inches",
		}

		data = CharacterData {
			Characters: filteredCharacters,
			Metadata: metadata,
		}
	} else if filterParam == "f" {
		var totalHeight uint64 = 0

		for _, character := range characters {
			if (character.Gender == "female") {
				filteredCharacters = append(filteredCharacters, character)

				h, _ := strconv.ParseUint(character.Height, 10, 64)
				totalHeight += h
			}
		}

		ftIn := float64(totalHeight) / 30.48
		ft := int64(ftIn)
		in := (ftIn - float64(int64(ft))) * 12

		metadata := CharacterMetaData {
			TotalNumber: len(filteredCharacters),
			TotalHeightCM: fmt.Sprintf("%dcm", totalHeight),
			TotalHeightFT: fmt.Sprintf("%dft and ", ft) + fmt.Sprintf("%.2f", in) + "inches",
		}

		data = CharacterData {
			Characters: filteredCharacters,
			Metadata: metadata,
		}
	} else {
		ftIn := float64(totalHeight) / 30.48
		ft := int64(ftIn)
		in := (ftIn - float64(int64(ft))) * 12

		metadata := CharacterMetaData {
			TotalNumber: len(characters),
			TotalHeightCM: fmt.Sprintf("%dcm", totalHeight),
			TotalHeightFT: fmt.Sprintf("%dft and ", ft) + fmt.Sprintf("%.2f", in) + "inches",
		}

		data = CharacterData {
			Characters: characters,
			Metadata: metadata,
		}
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status": "success",
		"message": "characters retrieved successfully",
		"data": data,
	})

	return
}