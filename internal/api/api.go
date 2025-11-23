package api

func InternalError() ErrorResponseError {
	return ErrorResponseError{
		Code:    ErrorResponseErrorCodeINTERNALERROR,
		Message: "Internal server error",
	}
}
