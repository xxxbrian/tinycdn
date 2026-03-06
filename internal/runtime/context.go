package runtime

import (
	"context"
)

type RequestContext struct {
	Site *CompiledSite
	Rule *CompiledRule
}

type requestContextKey struct{}

func ContextWithRequestContext(ctx context.Context, requestContext RequestContext) context.Context {
	return context.WithValue(ctx, requestContextKey{}, requestContext)
}

func RequestContextFrom(ctx context.Context) (RequestContext, bool) {
	value := ctx.Value(requestContextKey{})
	if value == nil {
		return RequestContext{}, false
	}

	requestContext, ok := value.(RequestContext)
	return requestContext, ok
}
