module github.com/zarielnd/file-management-service-go/services/file-server

go 1.26.5

require (
	github.com/google/uuid v1.6.0
	github.com/zarielnd/file-management-service-go/gen v0.0.0
	github.com/joho/godotenv v1.5.1
)

require (
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.39.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/grpc v1.83.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/zarielnd/file-management-service-go/gen => ../../gen
