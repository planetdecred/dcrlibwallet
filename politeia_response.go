package dcrlibwallet

const (
	// Error status codes
	ErrorStatusInvalid                     uint16 = 0
	ErrorStatusInvalidPassword             uint16 = 1
	ErrorStatusMalformedEmail              uint16 = 2
	ErrorStatusVerificationTokenInvalid    uint16 = 3
	ErrorStatusVerificationTokenExpired    uint16 = 4
	ErrorStatusProposalMissingFiles        uint16 = 5
	ErrorStatusProposalNotFound            uint16 = 6
	ErrorStatusProposalDuplicateFilenames  uint16 = 7
	ErrorStatusProposalInvalidTitle        uint16 = 8
	ErrorStatusMaxMDsExceededPolicy        uint16 = 9
	ErrorStatusMaxImagesExceededPolicy     uint16 = 10
	ErrorStatusMaxMDSizeExceededPolicy     uint16 = 11
	ErrorStatusMaxImageSizeExceededPolicy  uint16 = 12
	ErrorStatusMalformedPassword           uint16 = 13
	ErrorStatusCommentNotFound             uint16 = 14
	ErrorStatusInvalidFilename             uint16 = 15
	ErrorStatusInvalidFileDigest           uint16 = 16
	ErrorStatusInvalidBase64               uint16 = 17
	ErrorStatusInvalidMIMEType             uint16 = 18
	ErrorStatusUnsupportedMIMEType         uint16 = 19
	ErrorStatusInvalidPropStatusTransition uint16 = 20
	ErrorStatusInvalidPublicKey            uint16 = 21
	ErrorStatusNoPublicKey                 uint16 = 22
	ErrorStatusInvalidSignature            uint16 = 23
	ErrorStatusInvalidInput                uint16 = 24
	ErrorStatusInvalidSigningKey           uint16 = 25
	ErrorStatusCommentLengthExceededPolicy uint16 = 26
	ErrorStatusUserNotFound                uint16 = 27
	ErrorStatusWrongStatus                 uint16 = 28
	ErrorStatusNotLoggedIn                 uint16 = 29
	ErrorStatusUserNotPaid                 uint16 = 30
	ErrorStatusReviewerAdminEqualsAuthor   uint16 = 31
	ErrorStatusMalformedUsername           uint16 = 32
	ErrorStatusDuplicateUsername           uint16 = 33
	ErrorStatusVerificationTokenUnexpired  uint16 = 34
	ErrorStatusCannotVerifyPayment         uint16 = 35
	ErrorStatusDuplicatePublicKey          uint16 = 36
	ErrorStatusInvalidPropVoteStatus       uint16 = 37
	ErrorStatusUserLocked                  uint16 = 38
	ErrorStatusNoProposalCredits           uint16 = 39
	ErrorStatusInvalidUserManageAction     uint16 = 40
	ErrorStatusUserActionNotAllowed        uint16 = 41
	ErrorStatusWrongVoteStatus             uint16 = 42
	ErrorStatusCannotVoteOnPropComment     uint16 = 44
	ErrorStatusChangeMessageCannotBeBlank  uint16 = 45
	ErrorStatusCensorReasonCannotBeBlank   uint16 = 46
	ErrorStatusCannotCensorComment         uint16 = 47
	ErrorStatusUserNotAuthor               uint16 = 48
	ErrorStatusVoteNotAuthorized           uint16 = 49
	ErrorStatusVoteAlreadyAuthorized       uint16 = 50
	ErrorStatusInvalidAuthVoteAction       uint16 = 51
	ErrorStatusUserDeactivated             uint16 = 52
	ErrorStatusInvalidPropVoteBits         uint16 = 53
	ErrorStatusInvalidPropVoteParams       uint16 = 54
	ErrorStatusEmailNotVerified            uint16 = 55
	ErrorStatusInvalidUUID                 uint16 = 56
	ErrorStatusInvalidLikeCommentAction    uint16 = 57
	ErrorStatusInvalidCensorshipToken      uint16 = 58
	ErrorStatusEmailAlreadyVerified        uint16 = 59
	ErrorStatusNoProposalChanges           uint16 = 60
	ErrorStatusMaxProposalsExceededPolicy  uint16 = 61
	ErrorStatusDuplicateComment            uint16 = 62
	ErrorStatusInvalidLogin                uint16 = 63
	ErrorStatusCommentIsCensored           uint16 = 64
	ErrorStatusInvalidProposalVersion      uint16 = 65

	// Proposal vote status codes
	PropVoteStatusInvalid       uint8 = 0 // Invalid vote status
	PropVoteStatusNotAuthorized uint8 = 1 // Vote has not been authorized by author
	PropVoteStatusAuthorized    uint8 = 2 // Vote has been authorized by author
	PropVoteStatusStarted       uint8 = 3 // Proposal vote has been started
	PropVoteStatusFinished      uint8 = 4 // Proposal vote has been finished
	PropVoteStatusDoesntExist   uint8 = 5 // Proposal doesn't exist
)

var (
	// ErrorStatus converts error status codes to human readable text.
	ErrorStatus = map[uint16]string{
		ErrorStatusInvalid:                     "invalid error status",
		ErrorStatusInvalidPassword:             "invalid password",
		ErrorStatusMalformedEmail:              "malformed email",
		ErrorStatusVerificationTokenInvalid:    "invalid verification token",
		ErrorStatusVerificationTokenExpired:    "expired verification token",
		ErrorStatusProposalMissingFiles:        "missing proposal files",
		ErrorStatusProposalNotFound:            "proposal not found",
		ErrorStatusProposalDuplicateFilenames:  "duplicate proposal files",
		ErrorStatusProposalInvalidTitle:        "invalid proposal title",
		ErrorStatusMaxMDsExceededPolicy:        "maximum markdown files exceeded",
		ErrorStatusMaxImagesExceededPolicy:     "maximum image files exceeded",
		ErrorStatusMaxMDSizeExceededPolicy:     "maximum markdown file size exceeded",
		ErrorStatusMaxImageSizeExceededPolicy:  "maximum image file size exceeded",
		ErrorStatusMalformedPassword:           "malformed password",
		ErrorStatusCommentNotFound:             "comment not found",
		ErrorStatusInvalidFilename:             "invalid filename",
		ErrorStatusInvalidFileDigest:           "invalid file digest",
		ErrorStatusInvalidBase64:               "invalid base64 file content",
		ErrorStatusInvalidMIMEType:             "invalid MIME type detected for file",
		ErrorStatusUnsupportedMIMEType:         "unsupported MIME type for file",
		ErrorStatusInvalidPropStatusTransition: "invalid proposal status",
		ErrorStatusInvalidPublicKey:            "invalid public key",
		ErrorStatusNoPublicKey:                 "no active public key",
		ErrorStatusInvalidSignature:            "invalid signature",
		ErrorStatusInvalidInput:                "invalid input",
		ErrorStatusInvalidSigningKey:           "invalid signing key",
		ErrorStatusCommentLengthExceededPolicy: "maximum comment length exceeded",
		ErrorStatusUserNotFound:                "user not found",
		ErrorStatusWrongStatus:                 "wrong proposal status",
		ErrorStatusNotLoggedIn:                 "user not logged in",
		ErrorStatusUserNotPaid:                 "user hasn't paid paywall",
		ErrorStatusReviewerAdminEqualsAuthor:   "user cannot change the status of his own proposal",
		ErrorStatusMalformedUsername:           "malformed username",
		ErrorStatusDuplicateUsername:           "duplicate username",
		ErrorStatusVerificationTokenUnexpired:  "verification token not yet expired",
		ErrorStatusCannotVerifyPayment:         "cannot verify payment at this time",
		ErrorStatusDuplicatePublicKey:          "public key already taken by another user",
		ErrorStatusInvalidPropVoteStatus:       "invalid proposal vote status",
		ErrorStatusUserLocked:                  "user locked due to too many login attempts",
		ErrorStatusNoProposalCredits:           "no proposal credits",
		ErrorStatusInvalidUserManageAction:     "invalid user edit action",
		ErrorStatusUserActionNotAllowed:        "user action is not allowed",
		ErrorStatusWrongVoteStatus:             "wrong proposal vote status",
		ErrorStatusCannotVoteOnPropComment:     "cannot vote on proposal comment",
		ErrorStatusChangeMessageCannotBeBlank:  "status change message cannot be blank",
		ErrorStatusCensorReasonCannotBeBlank:   "censor comment reason cannot be blank",
		ErrorStatusCannotCensorComment:         "cannot censor comment",
		ErrorStatusUserNotAuthor:               "user is not the proposal author",
		ErrorStatusVoteNotAuthorized:           "vote has not been authorized",
		ErrorStatusVoteAlreadyAuthorized:       "vote has already been authorized",
		ErrorStatusInvalidAuthVoteAction:       "invalid authorize vote action",
		ErrorStatusUserDeactivated:             "user account is deactivated",
		ErrorStatusInvalidPropVoteBits:         "invalid proposal vote option bits",
		ErrorStatusInvalidPropVoteParams:       "invalid proposal vote parameters",
		ErrorStatusEmailNotVerified:            "email address is not verified",
		ErrorStatusInvalidUUID:                 "invalid user UUID",
		ErrorStatusInvalidLikeCommentAction:    "invalid like comment action",
		ErrorStatusInvalidCensorshipToken:      "invalid proposal censorship token",
		ErrorStatusEmailAlreadyVerified:        "email address is already verified",
		ErrorStatusNoProposalChanges:           "no changes found in proposal",
		ErrorStatusDuplicateComment:            "duplicate comment",
		ErrorStatusInvalidLogin:                "invalid login credentials",
		ErrorStatusCommentIsCensored:           "comment is censored",
		ErrorStatusInvalidProposalVersion:      "invalid proposal version",
	}
)
