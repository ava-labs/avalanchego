package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

func main() {
	// Define command-line flags
	appID := flag.String("app-id", "", "GitHub App App ID")
	privateKeyPath := flag.String("private-key-path", "", "Path to the GitHub App private key file")
	expiryDuration := flag.Duration("expiry", 10*time.Minute, "JWT expiration duration (e.g., 10m, 1h)")

	// Parse command-line flags
	flag.Parse()

	// Validate required flags
	if *appID == "" || *privateKeyPath == "" {
		log.Fatalf("Both app-id and private-key-path are required")
	}

	// Read the private key file
	privateKeyData, err := ioutil.ReadFile(*privateKeyPath)
	if err != nil {
		log.Fatalf("Error reading private key file: %v", err)
	}

	// Parse the private key
	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM(privateKeyData)
	if err != nil {
		log.Fatalf("Error parsing private key: %v", err)
	}

	// Create the JWT claims
	claims := jwt.MapClaims{
		"iat": time.Now().Unix(),                      // Issued at time
		"exp": time.Now().Add(*expiryDuration).Unix(), // JWT expiration time
		"iss": *appID,                                 // GitHub App App ID
	}

	// Create the JWT
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	jwtToken, err := token.SignedString(privateKey)
	if err != nil {
		log.Fatalf("Error signing JWT: %v", err)

	}

	// Output the JWT token
	fmt.Print(jwtToken)
}
