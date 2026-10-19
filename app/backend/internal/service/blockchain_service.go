package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/vnkmasc/Kmasc/app/backend/internal/common"
	"github.com/vnkmasc/Kmasc/app/backend/internal/models"
	"github.com/vnkmasc/Kmasc/app/backend/internal/repository"
	"github.com/vnkmasc/Kmasc/app/backend/pkg/blockchain"
	"github.com/vnkmasc/Kmasc/app/backend/pkg/database"
	"github.com/vnkmasc/Kmasc/app/backend/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type BlockchainService interface {
	VerifyEDiplomaAnchor(ctx context.Context, diplomaID primitive.ObjectID) (*models.BlockchainAnchorVerification, error)
	// PushToBlockchain anchors the signed diplomas of one faculty of one issuance round.
	PushToBlockchain(ctx context.Context, universityID string, scope bson.M, certificateType, course string) (int, error)
	// PushRevocationsToBlockchain records on Fabric the revocation of anchored diplomas in scope.
	PushRevocationsToBlockchain(ctx context.Context, universityID string, scope bson.M) (*models.PushRevocationResult, error)
}

type blockchainService struct {
	templateRepo   repository.TemplateRepository
	ediplomaRepo   repository.EDiplomaRepository
	userRepo       repository.UserRepository
	authRepo       repository.AuthRepository
	facultyRepo    repository.FacultyRepository
	universityRepo repository.UniversityRepository
	fabricClient   *blockchain.FabricClient
	minioClient    *database.MinioClient
	pqcTxSigner    *PQCTransactionSigner
	emailSender    utils.EmailSender
}

func NewBlockchainService(
	templateRepo repository.TemplateRepository,
	ediplomaRepo repository.EDiplomaRepository,
	userRepo repository.UserRepository,
	authRepo repository.AuthRepository,
	facultyRepo repository.FacultyRepository,
	universityRepo repository.UniversityRepository,
	fabricClient *blockchain.FabricClient,
	minioClient *database.MinioClient,
	pqcTxSigner *PQCTransactionSigner,
	emailSender utils.EmailSender,
) BlockchainService {
	return &blockchainService{
		ediplomaRepo:   ediplomaRepo,
		userRepo:       userRepo,
		authRepo:       authRepo,
		facultyRepo:    facultyRepo,
		universityRepo: universityRepo,
		fabricClient:   fabricClient,
		minioClient:    minioClient,
		pqcTxSigner:    pqcTxSigner,
		templateRepo:   templateRepo,
		emailSender:    emailSender,
	}
}

func (s *blockchainService) PushToBlockchain(
	ctx context.Context,
	universityID string,
	scope bson.M,
	certificateType, course string,
) (int, error) {
	// One faculty, or the whole round when the scope names no faculty.
	facultyIDStr := ""
	if facultyID, ok := scope["faculty_id"].(primitive.ObjectID); ok {
		facultyIDStr = facultyID.Hex()
	}
	filter := bson.M{}
	for k, v := range scope {
		filter[k] = v
	}
	filter["issued"] = true
	if certificateType != "" {
		filter["certificate_type"] = bson.M{"$regex": regexp.QuoteMeta(certificateType), "$options": "i"}
	}
	if course != "" {
		filter["course"] = bson.M{"$regex": regexp.QuoteMeta(course), "$options": "i"}
	}

	// Lấy danh sách EDiploma
	ediplomas, err := s.ediplomaRepo.FindByDynamicFilter(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("failed to load eDiplomas: %w", err)
	}
	if len(ediplomas) == 0 {
		return 0, common.ErrNoDiplomas
	}
	log.Printf("[BlockchainService] Found %d eDiplomas", len(ediplomas))

	// Gom hash
	infoHashes := []string{}
	fileHashes := []string{}
	hashMap := map[string]*models.EDiploma{} // map hash -> EDiploma để sinh proof sau

	for _, ed := range ediplomas {
		// The reported issuance flow is generate-and-sign. Only diplomas with
		// a completed PQC proof and a PDF hash may enter an on-chain batch.
		if ed.PQCProof != nil && strings.TrimSpace(ed.EDiplomaFileHash) != "" && ed.Issued && !ed.OnBlockchain && !ed.Revoked {
			hInfo, err := hashEDiplomaInfo(ed)
			if err != nil {
				log.Printf("[BlockchainService] hash failed for %s: %v", ed.StudentCode, err)
				continue
			}
			infoHashes = append(infoHashes, hInfo)
			hashMap[hInfo] = ed

			if ed.EDiplomaFileHash != "" {
				fileHashes = append(fileHashes, ed.EDiplomaFileHash)
			}
		}
	}

	if len(infoHashes) == 0 {
		return 0, common.ErrNoValidDiplomas
	}
	log.Printf("[BlockchainService] Info hashes: %+v", infoHashes)
	log.Printf("[BlockchainService] File hashes: %+v", fileHashes)

	// Tạo Merkle root
	merkleInfoTree := models.NewMerkleTreeFromStrings(infoHashes)
	merkleInfoRoot := merkleInfoTree.RootHash()
	log.Printf("[BlockchainService] Merkle Info Root: %s", merkleInfoRoot)

	merkleFileRoot := "no_file_hash"
	if len(fileHashes) > 0 {
		merkleFileTree := models.NewMerkleTreeFromStrings(fileHashes)
		merkleFileRoot = merkleFileTree.RootHash()
		log.Printf("[BlockchainService] Merkle File Root: %s", merkleFileRoot)
	}

	// The Merkle root makes each issuance batch immutable and unique. Reusing a
	// filter-based ID allowed a later issuance to overwrite the previous root.
	batchID := fmt.Sprintf("EDIP-%s-%s", universityID, merkleInfoRoot)
	log.Printf("[BlockchainService] BatchID = %s", batchID)

	batchOnChain := models.EDiplomaBatchOnChain{
		BatchID:           batchID,
		UniversityID:      universityID,
		FacultyID:         facultyIDStr,
		CertificateType:   certificateType,
		Course:            course,
		AggregateInfoHash: merkleInfoRoot,
		AggregateFileHash: merkleFileRoot,
		Count:             len(infoHashes),
	}
	if s.pqcTxSigner == nil {
		return 0, errors.New("PQC transaction signer is not configured")
	}
	if err := s.pqcTxSigner.SignEDiplomaBatch(ctx, &batchOnChain); err != nil {
		return 0, fmt.Errorf("sign blockchain transaction with PQC: %w", err)
	}

	// Push lên blockchain
	txID, err := s.fabricClient.IssueEDiplomaBatch(batchOnChain)
	if err != nil {
		return 0, fmt.Errorf("push blockchain failed: %w", err)
	}
	log.Printf("[BlockchainService] TxID = %s", txID)

	// Update DB với log chi tiết
	updatedCount := 0
	for hInfo, ed := range hashMap {
		proof := merkleInfoTree.GetProof(hInfo)
		if len(proof) == 0 {
			log.Printf("[⚠️ BlockchainService] Proof is EMPTY for %s (hash=%s)", ed.StudentCode, hInfo)
		} else {
			log.Printf("[✅ BlockchainService] Proof for %s (%s): %+v", ed.StudentCode, hInfo, proof)
		}

		verifyObj := &models.OnBlockchainVerify{
			UniversityID:    universityID,
			FacultyID:       facultyIDStr,
			CertificateType: certificateType,
			Course:          course,
		}
		update := bson.M{
			"$set": bson.M{
				"on_blockchain":        true,
				"transaction_id":       txID,
				"batch_id":             batchID,
				"merkle_proof":         proof,
				"on_blockchain_verify": verifyObj,
				"updated_at":           time.Now(),
			},
		}

		err := s.ediplomaRepo.UpdateByID(ctx, ed.ID, update)
		if err != nil {
			log.Printf("[❌ BlockchainService] Failed to update %s: %v", ed.StudentCode, err)
			continue
		}
		log.Printf("[🟢 BlockchainService] Updated %s successfully", ed.StudentCode)
		updatedCount++
		s.notifyEDiplomaIssued(ctx, ed)

	}

	log.Printf("[BlockchainService] Total updated records: %d", updatedCount)
	return updatedCount, nil
}

func (s *blockchainService) notifyEDiplomaIssued(ctx context.Context, diploma *models.EDiploma) {
	if s.emailSender == nil || diploma == nil || diploma.OnBlockchain || !diploma.Issued || !diploma.Signed || diploma.PQCProof == nil || strings.TrimSpace(diploma.EDiplomaFileHash) == "" {
		return
	}
	_, recipients := studentRecipients(ctx, s.userRepo, s.authRepo, diploma.UserID)
	if len(recipients) == 0 {
		return
	}
	verifyURL := fmt.Sprintf("%s/verify/%s", appURL(), diploma.ID.Hex())
	body := fmt.Sprintf("Xin chào %s,\n\nVăn bằng số của bạn đã được cấp, ký số hậu lượng tử %s và ghi nhận trên Blockchain.\n\nMã sinh viên: %s\nTên văn bằng: %s\nNgày cấp: %s\nSố hiệu: %s\n\nTra cứu và xác minh công khai (có mã QR): %s\nĐăng nhập tài khoản sinh viên để xem và tải bản PDF.\n\nEmail này chỉ mang tính thông báo; hệ thống không bao giờ gửi mật khẩu hay khóa bí mật qua email.", diploma.FullName, diploma.PQCProof.Algorithm, diploma.StudentCode, diploma.Name, diploma.IssueDate.Format("02/01/2006"), diploma.SerialNumber, verifyURL)
	go func() {
		for _, to := range recipients {
			if err := s.emailSender.SendEmail(to, "Thông báo cấp văn bằng số - VBS PQC", body); err != nil {
				log.Printf("failed to send diploma notification for %s: %v", diploma.ID.Hex(), err)
			}
		}
	}()
}

func hashEDiplomaInfo(ed *models.EDiploma) (string, error) {
	if ed.PQCProof != nil && ed.PQCProof.AnchorVersion >= models.PQCAnchorVersion {
		return hashPQCEDiplomaAnchor(ed)
	}

	data := ed.StudentCode +
		ed.FullName +
		ed.CertificateType +
		ed.Course +
		ed.EducationType +
		fmt.Sprintf("%.2f", ed.GPA) +
		ed.GraduationRank +
		ed.IssueDate.Format("2006-01-02") +
		ed.SerialNumber +
		ed.RegistrationNumber
	// New PQC-enabled records bind the immutable proof to the Merkle root.
	// Legacy records deliberately keep their old hash input for compatibility.
	if ed.PQCProof != nil {
		signatureHash := sha256.Sum256([]byte(ed.PQCProof.Signature))
		data += ed.PQCProof.ManifestHash +
			ed.PQCProof.KeyID +
			hex.EncodeToString(signatureHash[:])
	}

	h := sha256.New()
	_, err := h.Write([]byte(data))
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashPQCEDiplomaAnchor(ed *models.EDiploma) (string, error) {
	canonical, err := pqcAnchorPayload(ed)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

// pqcAnchorPayload is the canonical JSON whose SHA-256 is the Merkle leaf of a
// PQC-signed diploma. The chaincode re-hashes it when a revocation is recorded.
func pqcAnchorPayload(ed *models.EDiploma) ([]byte, error) {
	signatureBytes, err := base64.StdEncoding.DecodeString(ed.PQCProof.Signature)
	if err != nil {
		return nil, fmt.Errorf("decode PQC signature for blockchain anchor: %w", err)
	}
	signatureHash := sha256.Sum256(signatureBytes)
	payload := struct {
		Domain               string `json:"domain"`
		AnchorVersion        int    `json:"anchor_version"`
		CredentialID         string `json:"credential_id"`
		UniversityID         string `json:"university_id"`
		Algorithm            string `json:"algorithm"`
		KeyID                string `json:"key_id"`
		PublicKeyFingerprint string `json:"public_key_fingerprint"`
		ManifestHash         string `json:"manifest_hash"`
		SignatureHash        string `json:"signature_hash"`
		FileHash             string `json:"file_hash"`
	}{
		Domain:               "PQC-EDIPLOMA-ANCHOR-V1",
		AnchorVersion:        ed.PQCProof.AnchorVersion,
		CredentialID:         ed.ID.Hex(),
		UniversityID:         ed.UniversityID.Hex(),
		Algorithm:            ed.PQCProof.Algorithm,
		KeyID:                ed.PQCProof.KeyID,
		PublicKeyFingerprint: ed.PQCProof.PublicKeyFingerprint,
		ManifestHash:         ed.PQCProof.ManifestHash,
		SignatureHash:        hex.EncodeToString(signatureHash[:]),
		FileHash:             strings.ToLower(ed.EDiplomaFileHash),
	}
	return json.Marshal(payload)
}

func aggregateHashes(hashes []string) (string, error) {
	if len(hashes) == 0 {
		return "", nil
	}
	combined := strings.Join(hashes, "")
	h := sha256.New()
	_, err := h.Write([]byte(combined))
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s *blockchainService) VerifyEDiplomaAnchor(
	ctx context.Context,
	diplomaID primitive.ObjectID,
) (*models.BlockchainAnchorVerification, error) {
	result := &models.BlockchainAnchorVerification{Status: "not_anchored"}
	ed, err := s.ediplomaRepo.FindByID(ctx, diplomaID)
	if err != nil {
		return nil, err
	}
	if !ed.OnBlockchain {
		return result, nil
	}
	result.BatchID = ed.BatchID
	result.TransactionID = ed.TransactionID
	if result.BatchID == "" {
		result.Status = "inconsistent"
		result.Error = "diploma is marked on-chain but has no batch ID"
		return result, nil
	}
	if len(ed.MerkleProof) == 0 && !isImmutableMerkleBatchID(result.BatchID, ed.UniversityID) {
		result.Status = "legacy_batch"
		result.Error = "legacy aggregate batch has no individual Merkle proof"
		return result, nil
	}
	if s.fabricClient == nil {
		result.Status = "unavailable"
		result.Error = "Fabric connection is unavailable"
		return result, nil
	}

	leaf, err := hashEDiplomaInfo(ed)
	if err != nil {
		result.Status = "invalid_local_proof"
		result.Error = err.Error()
		return result, nil
	}
	result.ComputedLeaf = leaf
	batch, err := s.fabricClient.GetEDiplomaBatch(result.BatchID)
	if err != nil {
		result.Status = "unavailable"
		result.Error = err.Error()
		return result, nil
	}
	result.Checked = true
	result.MerkleRoot = batch.AggregateInfoHash
	if batch.UniversityID != "" && batch.UniversityID != ed.UniversityID.Hex() {
		result.Status = "mismatch"
		result.Error = "blockchain batch belongs to another university"
		return result, nil
	}
	if batch.PQCTransaction != nil {
		result.PQCTransactionChecked = true
		result.PQCTransactionKeyID = batch.PQCTransaction.KeyID
		if s.pqcTxSigner == nil {
			result.Status = "unavailable"
			result.Error = "PQC transaction verifier is unavailable"
			return result, nil
		}
		if err := s.pqcTxSigner.VerifyEDiplomaBatch(ctx, batch); err != nil {
			result.Status = "mismatch"
			result.Error = err.Error()
			return result, nil
		}
		result.PQCTransactionValid = true
	} else {
		result.Status = "missing_pqc_transaction"
		result.Error = "batch has no PQC transaction attestation"
		return result, nil
	}
	// A one-leaf Merkle tree legitimately has an empty proof.
	result.Valid = models.VerifyProof(leaf, ed.MerkleProof, result.MerkleRoot)
	if result.Valid {
		result.Status = "verified"
	} else {
		result.Status = "mismatch"
		result.Error = "Merkle proof does not match the root stored on Fabric"
	}
	s.checkLedgerRevocation(ed, result)
	return result, nil
}

// checkLedgerRevocation reads the revocation of an anchored diploma from Fabric. A
// ledger revocation cannot be undone by editing the database.
func (s *blockchainService) checkLedgerRevocation(ed *models.EDiploma, result *models.BlockchainAnchorVerification) {
	status, err := s.fabricClient.GetEDiplomaRevocation(ed.ID.Hex())
	switch {
	case errors.Is(err, blockchain.ErrNotRevokedOnChain):
		result.RevocationStatus = "not_revoked"
	case err != nil:
		// For example a chaincode version without revocation support.
		result.RevocationStatus = "unavailable"
	default:
		result.RevocationStatus = "revoked"
		result.RevokedOnChain = true
		result.RevocationID = status.RevocationID
		result.RevocationTxID = status.TxID
		result.RevokedAtOnChain = status.RevokedAt
	}
}

func isImmutableMerkleBatchID(batchID string, universityID primitive.ObjectID) bool {
	prefix := "EDIP-" + universityID.Hex() + "-"
	root := strings.TrimPrefix(batchID, prefix)
	if root == batchID || len(root) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(root)
	return err == nil
}
