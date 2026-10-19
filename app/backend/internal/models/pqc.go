package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	// FIPS 204 parameter sets supported by the signing service.
	PQCAlgorithmMLDSA44   = "ML-DSA-44"
	PQCAlgorithmMLDSA65   = "ML-DSA-65"
	PQCAlgorithmMLDSA87   = "ML-DSA-87"
	PQCDefaultAlgorithm   = PQCAlgorithmMLDSA65
	PQCProofVersion       = 1
	PQCAnchorVersion      = 1
	PQCSignatureContext   = "PQC-EDIPLOMA-V1"
	PQCTransactionVersion = 1
	PQCTransactionContext = "VBS-PQC-FABRIC-TX-V1"

	PQCKeyPending = "pending"
	PQCKeyActive  = "active"
	PQCKeyRetired = "retired"
	PQCKeyRevoked = "revoked"
)

// PQCKey stores a versioned university signing key. Private key material is
// encrypted with the deployment master key and is never returned by the API.
// Retired and revoked records are deliberately retained for historical proof
// verification.
type PQCKey struct {
	ID                    primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UniversityID          primitive.ObjectID `bson:"university_id" json:"university_id"`
	Name                  string             `bson:"name" json:"name"`
	Algorithm             string             `bson:"algorithm" json:"algorithm"`
	Status                string             `bson:"status" json:"status"`
	PublicKey             string             `bson:"public_key" json:"public_key"`
	PublicKeyFingerprint  string             `bson:"public_key_fingerprint" json:"public_key_fingerprint"`
	EncryptedPrivateKey   string             `bson:"encrypted_private_key" json:"-"`
	PrivateKeyNonce       string             `bson:"private_key_nonce" json:"-"`
	CreatedAt             time.Time          `bson:"created_at" json:"created_at"`
	ActivatedAt           *time.Time         `bson:"activated_at,omitempty" json:"activated_at,omitempty"`
	RetiredAt             *time.Time         `bson:"retired_at,omitempty" json:"retired_at,omitempty"`
	RevokedAt             *time.Time         `bson:"revoked_at,omitempty" json:"revoked_at,omitempty"`
	CompromiseEffectiveAt *time.Time         `bson:"compromise_effective_at,omitempty" json:"compromise_effective_at,omitempty"`
	RevocationReason      string             `bson:"revocation_reason,omitempty" json:"revocation_reason,omitempty"`
}

// PQCCredentialManifest is a deterministic, versioned representation of the
// diploma facts covered by the signature. It is marshalled from a struct (not
// a map), which keeps field ordering stable across signing and verification.
type PQCCredentialManifest struct {
	Schema             string `json:"schema"`
	CredentialID       string `json:"credential_id"`
	UniversityID       string `json:"university_id"`
	FacultyID          string `json:"faculty_id"`
	StudentCode        string `json:"student_code"`
	FullName           string `json:"full_name"`
	CertificateType    string `json:"certificate_type"`
	Course             string `json:"course"`
	EducationType      string `json:"education_type"`
	GPA                string `json:"gpa"`
	GraduationRank     string `json:"graduation_rank"`
	IssueDate          string `json:"issue_date"`
	SerialNumber       string `json:"serial_number"`
	RegistrationNumber string `json:"registration_number"`
	FileSHA256         string `json:"file_sha256"`
	SignedAt           string `json:"signed_at"`
}

// PQCProof is embedded in the diploma. The public key is referenced by an
// immutable key ID and fingerprint; the key record remains available after
// rotation/revocation so old proofs can still be evaluated at signing time.
type PQCProof struct {
	Version              int       `bson:"version" json:"version"`
	AnchorVersion        int       `bson:"anchor_version,omitempty" json:"anchor_version,omitempty"`
	Algorithm            string    `bson:"algorithm" json:"algorithm"`
	Context              string    `bson:"context" json:"context"`
	KeyID                string    `bson:"key_id" json:"key_id"`
	PublicKeyFingerprint string    `bson:"public_key_fingerprint" json:"public_key_fingerprint"`
	ManifestHash         string    `bson:"manifest_hash" json:"manifest_hash"`
	Signature            string    `bson:"signature" json:"signature"`
	SignedAt             time.Time `bson:"signed_at" json:"signed_at"`
}

// PQCTransactionAttestation binds the Fabric payload to an ML-DSA-65 key.
// Fabric still authenticates and endorses the transaction with X.509; this
// attestation provides an independent post-quantum signature over the payload
// being anchored.
type PQCTransactionAttestation struct {
	Version              int       `json:"version"`
	Algorithm            string    `json:"algorithm"`
	Context              string    `json:"context"`
	KeyID                string    `json:"key_id"`
	PublicKey            string    `json:"public_key"`
	PublicKeyFingerprint string    `json:"public_key_fingerprint"`
	EnvelopeHash         string    `json:"envelope_hash"`
	Signature            string    `json:"signature"`
	SignedAt             time.Time `json:"signed_at"`
	Nonce                string    `json:"nonce"`
}

type PQCTransactionEnvelope struct {
	Version           int    `json:"version"`
	TransactionType   string `json:"transaction_type"`
	BatchID           string `json:"batch_id"`
	UniversityID      string `json:"university_id"`
	FacultyID         string `json:"faculty_id"`
	CertificateType   string `json:"certificate_type"`
	Course            string `json:"course"`
	AggregateInfoHash string `json:"aggregate_info_hash"`
	AggregateFileHash string `json:"aggregate_file_hash"`
	Count             int    `json:"count"`
	KeyID             string `json:"key_id"`
	SignedAt          string `json:"signed_at"`
	Nonce             string `json:"nonce"`
}

// BlockchainAnchorVerification describes the independent ledger check. The
// ledger stores only a Merkle root; diploma data, signatures and public keys
// remain off-chain.
type BlockchainAnchorVerification struct {
	Status                string `json:"status"`
	Checked               bool   `json:"checked"`
	Valid                 bool   `json:"valid"`
	BatchID               string `json:"batch_id,omitempty"`
	TransactionID         string `json:"transaction_id,omitempty"`
	MerkleRoot            string `json:"merkle_root,omitempty"`
	ComputedLeaf          string `json:"computed_leaf,omitempty"`
	PQCTransactionChecked bool   `json:"pqc_transaction_checked"`
	PQCTransactionValid   bool   `json:"pqc_transaction_valid"`
	PQCTransactionKeyID   string `json:"pqc_transaction_key_id,omitempty"`
	RevocationStatus      string `json:"revocation_status,omitempty"`
	RevokedOnChain        bool   `json:"revoked_on_chain"`
	RevocationID          string `json:"revocation_id,omitempty"`
	RevocationTxID        string `json:"revocation_tx_id,omitempty"`
	RevokedAtOnChain      string `json:"revoked_at_on_chain,omitempty"`
	Error                 string `json:"error,omitempty"`
}

type PQCVerificationResult struct {
	Valid                  bool                         `json:"valid"`
	SignatureValid         bool                         `json:"signature_valid"`
	CryptographicallyValid bool                         `json:"cryptographically_valid"`
	KeyValidAtSigning      bool                         `json:"key_valid_at_signing"`
	FileIntegrityChecked   bool                         `json:"file_integrity_checked"`
	FileIntegrityValid     bool                         `json:"file_integrity_valid"`
	StoredFileHash         string                       `json:"stored_file_hash,omitempty"`
	ComputedFileHash       string                       `json:"computed_file_hash,omitempty"`
	AssuranceLevel         string                       `json:"assurance_level"`
	Blockchain             BlockchainAnchorVerification `json:"blockchain"`
	CurrentKeyStatus       string                       `json:"current_key_status"`
	Algorithm              string                       `json:"algorithm"`
	KeyID                  string                       `json:"key_id"`
	ManifestHash           string                       `json:"manifest_hash"`
	SignedAt               time.Time                    `json:"signed_at"`
	Warning                string                       `json:"warning,omitempty"`
	Error                  string                       `json:"error,omitempty"`
}

type CreatePQCKeyRequest struct {
	Name      string `json:"name" binding:"required,min=3,max=100"`
	Algorithm string `json:"algorithm" binding:"omitempty,oneof=ML-DSA-44 ML-DSA-65 ML-DSA-87"`
}

type RevokePQCKeyRequest struct {
	Reason                string     `json:"reason" binding:"required,min=5,max=500"`
	CompromiseEffectiveAt *time.Time `json:"compromise_effective_at"`
}

type SignPQCFilterRequest struct {
	FacultyID       string `json:"faculty_id"`
	TemplateID      string `json:"template_id"`
	CertificateType string `json:"certificate_type"`
	Course          string `json:"course"`
	RoundID         string `json:"round_id"`
}
