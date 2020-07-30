package politeia

type Err struct {
	Code    uint16   `json:"errorcode"`
	Context []string `json:"errorcontext"`
}

type ServerVersion struct {
	Version int `json:"version"`
}

type ServerPolicy struct {
	ProposalListPageSize int `json:"proposallistpagesize"`
}

type ProposalFile struct {
	Name    string `json:"name"`
	Mime    string `json:"mime"`
	Digest  string `json:"digest"`
	Payload string `json:"payload"`
}

type ProposalMetaData struct {
	Name   string `json:"name"`
	LinkTo string `json:"linkto"`
	LinkBy int64  `json:"linkby"`
}

type ProposalCensorshipRecord struct {
	Token     string `json:"token"`
	Merkle    string `json:"merkle"`
	Signature string `json:"signature"`
}

type Proposal struct {
	Name             string                   `json:"name"`
	State            int                      `json:"state"`
	Status           int                      `json:"status"`
	Timestamp        int64                    `json:"timestamp"`
	UserID           string                   `json:"userid"`
	Username         string                   `json:"username"`
	PublicKey        string                   `json:"publickey"`
	Signature        string                   `json:"signature"`
	NumComments      int                      `json:"numcomments"`
	Version          string                   `json:"version"`
	PublishedAt      int64                    `json:"publishedat"`
	Files            []ProposalFile           `json:"files"`
	MetaData         []ProposalMetaData       `json:"metadata"`
	CensorshipRecord ProposalCensorshipRecord `json:"censorshiprecord"`
	VoteStatus       VoteStatus
}

type Proposals struct {
	Proposals []Proposal `json:"proposals"`
}

type ProposalResult struct {
	Proposal Proposal `json:"proposal"`
}

type VoteOption struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Bits        int    `json:"bits"`
}

type VoteOptionResult struct {
	Option        VoteOption `json:"option"`
	VotesReceived int64      `json:"votesreceived"`
}

type VoteStatus struct {
	Token              string             `json:"token"`
	Status             int                `json:"status"`
	TotalVotes         int                `json:"totalvotes"`
	OptionsResult      []VoteOptionResult `json:"optionsresult"`
	EndHeight          string             `json:"endheight"`
	BestBlock          string             `json:"bestblock"`
	NumOfEligibleVotes int                `json:"numofeligiblevotes"`
	QuorumPercentage   int                `json:"quorumpercentage"`
	PassPercentage     int                `json:"passpercentage"`
}

type VotesStatus struct {
	VotesStatus []VoteStatus `json:"votesstatus"`
}
