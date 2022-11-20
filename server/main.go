package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/appditto/MonKey/server/controller"
	"github.com/appditto/MonKey/server/database"
	"github.com/appditto/MonKey/server/image"
	"github.com/appditto/MonKey/server/utils"
	"github.com/gin-gonic/gin"
	"github.com/h2non/bimg"
	"github.com/joho/godotenv"
)

func CorsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin == "" {
			origin = "https://monkey.banano.cc"
		}
		c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, Content-Length, X-CSRF-Token, Token, session, Origin, Host, Connection, Accept-Encoding, Accept-Language, X-Requested-With, ResponseType")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Request.Header.Del("Origin")

		c.Next()
	}
}

// Generate random files
func RandFiles(count int, seed string, bg bool) {
	if _, err := os.Stat("randsvg"); os.IsNotExist(err) {
		os.Mkdir("randsvg", os.FileMode(0755))
	}
	for i := 0; i < count; i++ {
		address := utils.GenerateAddress()
		sha256 := utils.Sha256(address, seed)

		accessories, _ := image.GetAccessoriesForHash(sha256, bg)
		svg, _ := image.CombineSVG(accessories)
		os.WriteFile(fmt.Sprintf("randsvg/%s.svg", address), svg, os.FileMode(0644))
	}
}

func main() {
	// Get seed from env
	seed := utils.GetEnv("MONKEY_SEED", "1234567890")

	// Server options
	serverHost := flag.String("host", "127.0.0.1", "Host to listen on")
	serverPort := flag.Int("port", 8080, "Port to listen on")
	testAccessoryDistribution := flag.Bool("test-ad", false, "Test accessory distribution")
	randomFiles := flag.Int("rand-files", -1, "Generate this many random SVGs and output to randsvg folder")
	randomFilesBG := flag.Int("rand-files-bg", -1, "Generate random SVGs with background and output to randsvg folder")
	flag.Parse()

	if *testAccessoryDistribution {
		controller.TestAccessoryDistribution(seed)
		return
	} else if *randomFiles > 0 {
		fmt.Printf("Generating %d files in ./randsvg", *randomFiles)
		RandFiles(*randomFiles, seed, false)
		return
	} else if *randomFilesBG > 0 {
		fmt.Printf("Generating %d files in ./randsvg", *randomFilesBG)
		RandFiles(*randomFilesBG, seed, true)
		return
	}

	godotenv.Load()
	// Setup database conn
	config := &database.Config{
		Host:     "127.0.0.1",
		Port:     "54322",
		User:     "postgres",
		Password: "postgres",
		SSLMode:  "disable",
		DBName:   "monkey",
	}
	fmt.Println("🏡 Connecting to database...")
	db, err := database.NewConnection(config)
	if err != nil {
		panic(err)
	}

	fmt.Println("🦋 Running database migrations...")
	database.Migrate(db)

	// Setup channel for stats processing job
	statsChan := make(chan controller.StatsMessage, 100)

	// Setup libvips
	bimg.Initialize()
	bimg.VipsCacheSetMaxMem(0)
	bimg.VipsCacheSetMax(0)
	defer bimg.Shutdown()

	// Setup router
	router := gin.Default()
	router.Use(CorsMiddleware())
	// pprof.Register(router)

	// Setup natricon controller
	monkeyController := controller.MonkeyController{
		Seed:           seed,
		StatsChannel:   &statsChan,
		ImageConverter: &image.ImageConverter{},
	}
	statsController := controller.StatsController{
		DB: db,
	}

	// V1 API
	apiGroup := router.Group("/api/v1")
	// Stats
	apiGroup.GET("/stats", statsController.Stats)
	apiGroup.GET("/stats/monthly", statsController.StatsMonthly)
	// Address
	apiGroup.GET("/monkey/:address", monkeyController.GetBanano)
	apiGroup.POST("/monkey/dtl", monkeyController.MonkeyStats)
	// Testing
	if gin.IsDebugging() {
		apiGroup.GET("/random", monkeyController.GetRandomSvg)
	}

	// Start stats worker
	go statsController.StatsWorker(statsChan)

	// Run on 8080
	router.Run(fmt.Sprintf("%s:%d", *serverHost, *serverPort))
}
