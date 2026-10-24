package server

import "github.com/lucasfd3/Airlink/internal/model"

func publicTransfer(t model.Transfer) model.Transfer {
	t.Destination = ""
	if t.Error != "" {
		t.Error = "Transfer needs attention. Check the source and destination on the host."
	}
	return t
}
