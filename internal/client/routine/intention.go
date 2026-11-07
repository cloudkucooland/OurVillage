package routine

type Kind string

const (
	HomeAndRest Kind = "HOME_AND_REST"
	Visit       Kind = "VISIT"
)

type Phase string

const (
	Traveling    Phase = "TRAVELING"
	StartingRest Phase = "STARTING_REST"
	Resting      Phase = "RESTING"
)

type Intention struct {
	Kind          Kind   `json:"kind"`
	Purpose       string `json:"purpose"`
	DestinationID string `json:"destination_id"`
	Phase         Phase  `json:"phase"`
}
