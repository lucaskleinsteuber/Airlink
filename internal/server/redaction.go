package server

import "github.com/lucaskleinsteuber/Airlink/internal/model"

func publicTransfer(t model.Transfer) model.Transfer {
	t.Destination = ""
	t.Files = append([]model.File(nil), t.Files...)
	for i := range t.Files {
		t.Files[i] = publicFile(t.Files[i])
	}
	if t.Error != "" {
		t.Error = "Transfer needs attention. Check the source and destination on the host."
	}
	return t
}

func publicFile(f model.File) model.File {
	f.DestinationPath = ""
	f.DuplicateCandidates = nil
	return f
}
