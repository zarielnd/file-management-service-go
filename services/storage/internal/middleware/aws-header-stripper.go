package middleware

import (
	"context"

	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

func GCSCompatibleHeaders(stack *middleware.Stack) error {
	mw := middleware.FinalizeMiddlewareFunc("GCSCompat", func(
		ctx context.Context, in middleware.FinalizeInput, next middleware.FinalizeHandler,
	) (middleware.FinalizeOutput, middleware.Metadata, error) {
		if req, ok := in.Request.(*smithyhttp.Request); ok {
			req.Header.Del("amz-sdk-invocation-id")
			req.Header.Del("amz-sdk-request")
			req.Header.Del("Accept-Encoding")
		}
		return next.HandleFinalize(ctx, in)
	})

	// Try to place before Signing so headers are stripped before the signature is computed.
	// Fall back to adding at the start of Finalize if Signing isn't in the stack
	// (e.g. during presign operations).
	if err := stack.Finalize.Insert(mw, "Signing", middleware.Before); err != nil {
		return stack.Finalize.Add(mw, middleware.Before)
	}
	return nil
}
