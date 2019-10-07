package context

// Context specifies the context in which a feature toggle should be considered
// to be enabled or not.
type Context struct {
	// UserId is the the id of the user.
	UserId string

	// SessionId is the id of the session.
	SessionId string

	// RemoteAddress is the IP address of the machine.
	RemoteAddress string

	// Environment is the environment this application is running in.
	Environment string

	// AppName is the application name.
	AppName string

	// Properties is a map of additional properties.
	Properties map[string]string
}

func (ctx Context) Override(src Context) *Context {
	if src.UserId != "" {
		ctx.UserId = src.UserId
	}
	if src.SessionId != "" {
		ctx.SessionId = src.SessionId
	}
	if src.RemoteAddress != "" {
		ctx.RemoteAddress = src.RemoteAddress
	}
	if src.Environment != "" {
		ctx.Environment = src.Environment
	}
	if src.AppName != "" {
		ctx.AppName = src.AppName
	}
	if src.Properties != nil {
		ctx.Properties = src.Properties
	}

	return &ctx
}
