package update

import "time"

func freshCreatedAt() string {
	return time.Now().UTC().Format(time.RFC3339)
}
