module github.com/zarielnd/file-management-service-go/services/storage

go 1.26.5

replace github.com/zarielnd/file-management-service-go/gen => ../../gen

require (
	github.com/google/uuid v1.6.0
	github.com/zarielnd/file-management-service-go/gen v0.0.0-00010101000000-000000000000
	google.golang.org/grpc v1.83.0
	google.golang.org/protobuf v1.36.12
)

require (
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.39.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
)
