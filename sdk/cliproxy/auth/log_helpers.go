package auth

func hideAPIKey(value string) string {
	switch {
	case len(value) > 8:
		return value[:4] + "..." + value[len(value)-4:]
	case len(value) > 4:
		return value[:2] + "..." + value[len(value)-2:]
	case len(value) > 2:
		return value[:1] + "..." + value[len(value)-1:]
	default:
		return value
	}
}
