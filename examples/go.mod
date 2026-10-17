module github.com/didit-protocol/sdk-go/examples

go 1.25.0

require (
	github.com/didit-protocol/sdk-go v0.0.0-00010101000000-000000000000
	github.com/gin-gonic/gin v1.12.0
	github.com/go-chi/chi/v5 v5.5.0
	github.com/gofiber/fiber/v2 v2.52.15
	github.com/labstack/echo/v4 v4.16.0
)

// The examples always build against the SDK in this repository.
replace github.com/didit-protocol/sdk-go => ../
