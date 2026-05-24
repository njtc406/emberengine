package discovery

import (
	"encoding/json"
	"fmt"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"google.golang.org/protobuf/encoding/protojson"
)

type ServiceEntry struct {
	PID        json.RawMessage       `json:"pid"`
	Status     int32                 `json:"status"`
	Visibility def.ServiceVisibility `json:"visibility"`
}

func MarshalServiceEntry(pid *actor.PID, status int32, visibility def.ServiceVisibility) ([]byte, error) {
	pidData, err := actor.MarshalPIDJSON(pid)
	if err != nil {
		return nil, fmt.Errorf("marshal pid failed: %w", err)
	}
	return json.Marshal(&ServiceEntry{
		PID:        pidData,
		Status:     status,
		Visibility: visibility,
	})
}

func UnmarshalServiceEntry(data []byte) (*actor.PID, int32, def.ServiceVisibility, error) {
	var entry ServiceEntry
	if err := json.Unmarshal(data, &entry); err == nil && len(entry.PID) > 0 {
		var pid actor.PID
		if err := protojson.Unmarshal(entry.PID, &pid); err != nil {
			return nil, 0, def.ServiceVisibilityPrivate, fmt.Errorf("unmarshal service entry pid failed: %w", err)
		}
		pid.SyncMasterFlag()
		status := entry.Status
		if status == 0 {
			status = def.SvcStatusReady
		}
		visibility := entry.Visibility
		if visibility == 0 {
			// 0 is not a valid ServiceVisibility value. Treat it as legacy data
			// produced before ServiceEntry included an explicit visibility field.
			visibility = def.ServiceVisibilityCluster
		}
		return &pid, status, visibility, nil
	}

	var pid actor.PID
	if err := protojson.Unmarshal(data, &pid); err != nil {
		return nil, 0, def.ServiceVisibilityPrivate, fmt.Errorf("unmarshal pid failed: %w", err)
	}
	pid.SyncMasterFlag()
	return &pid, def.SvcStatusReady, def.ServiceVisibilityCluster, nil
}
