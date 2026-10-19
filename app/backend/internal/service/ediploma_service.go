package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/vnkmasc/Kmasc/app/backend/internal/common"
	"github.com/vnkmasc/Kmasc/app/backend/internal/mapper"
	"github.com/vnkmasc/Kmasc/app/backend/internal/models"
	"github.com/vnkmasc/Kmasc/app/backend/internal/repository"
	"github.com/vnkmasc/Kmasc/app/backend/pkg/database"
	"github.com/vnkmasc/Kmasc/app/backend/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type EDiplomaService interface {
	GetSimpleEDiplomasByUserID(ctx context.Context, userID primitive.ObjectID) ([]*models.EDiplomaSimpleResponse, error)
	GetEDiplomaFileByUniversityCode(ctx context.Context, ediplomaID primitive.ObjectID, universityCode string) (io.ReadCloser, string, error)
	GetEDiplomaFile(ctx context.Context, ediplomaID, universityID primitive.ObjectID) (io.ReadCloser, string, error)
	GetEDiplomaDTOByID(ctx context.Context, id primitive.ObjectID) (*models.EDiplomaResponse, error)
	GetByID(ctx context.Context, id string) (*models.EDiploma, error)
	CreateEDiploma(ctx context.Context, claims *utils.CustomClaims, req *models.CreateEDiplomaRequest) (*models.EDiploma, error)
	GenerateEDiploma(ctx context.Context, ediplomaIDStr, templateIDStr string) (*models.EDiploma, error)
	SearchEDiplomaDTOs(ctx context.Context, filter models.EDiplomaSearchFilter) ([]*models.EDiplomaResponse, int64, error)
	RevokeEDiploma(ctx context.Context, universityID, diplomaID primitive.ObjectID, reason string) error

	CreateRound(ctx context.Context, universityID primitive.ObjectID, req models.CreateIssuanceRoundRequest) (*models.IssuanceRound, error)
	SearchRounds(ctx context.Context, universityID primitive.ObjectID, query, facultyID string, limit int) ([]models.IssuanceRound, error)
	RequireRound(ctx context.Context, universityID primitive.ObjectID, roundID string) (*models.IssuanceRound, error)
	RoundScope(ctx context.Context, universityID primitive.ObjectID, facultyID, roundID string) (bson.M, error)
	AssignRound(ctx context.Context, universityID primitive.ObjectID, roundID, facultyID, course, certificateType string) (int64, error)
	RevokeRound(ctx context.Context, universityID primitive.ObjectID, facultyID, roundID, reason string) (int64, error)
}

func (s *eDiplomaService) RevokeEDiploma(ctx context.Context, universityID, diplomaID primitive.ObjectID, reason string) error {
	diploma, err := s.repo.FindByID(ctx, diplomaID)
	if err != nil || diploma == nil {
		return fmt.Errorf("ediploma not found")
	}
	if diploma.UniversityID != universityID {
		return fmt.Errorf("access denied")
	}
	if !diploma.Issued {
		return fmt.Errorf("only issued diplomas can be revoked")
	}
	if diploma.Revoked {
		return fmt.Errorf("ediploma is already revoked")
	}
	reason = strings.TrimSpace(reason)
	if len(reason) < 5 {
		return fmt.Errorf("revocation reason must be at least 5 characters")
	}
	revokedAt := time.Now()
	if err := s.repo.Revoke(ctx, diplomaID, reason, revokedAt); err != nil {
		return err
	}
	s.notifier.NotifyRevoked(ctx, []*models.EDiploma{diploma}, reason, revokedAt)
	return nil
}

type eDiplomaService struct {
	templateSampleRepo repository.TemplateSampleRepo
	universityRepo     repository.UniversityRepository
	majorRepo          repository.MajorRepository
	facultyRepo        repository.FacultyRepository
	repo               repository.EDiplomaRepository
	templateRepo       repository.TemplateRepository
	userRepo           repository.UserRepository
	minioClient        *database.MinioClient
	templateEngine     *models.TemplateEngine
	pdfGenerator       *utils.PDFGenerator
	pqcKeys            repository.PQCKeyRepository
	rounds             repository.IssuanceRoundRepository
	notifier           *StudentNotifier
}

func NewEDiplomaService(
	templateSampleRepo repository.TemplateSampleRepo,
	universityRepo repository.UniversityRepository,
	majorRepo repository.MajorRepository,
	facultyRepo repository.FacultyRepository,
	repo repository.EDiplomaRepository,
	templateRepo repository.TemplateRepository,
	userRepo repository.UserRepository,
	minioClient *database.MinioClient,
	templateEngine *models.TemplateEngine,
	pdfGenerator *utils.PDFGenerator,
	pqcKeys repository.PQCKeyRepository,
	rounds repository.IssuanceRoundRepository,
	notifier *StudentNotifier,
) *eDiplomaService {
	return &eDiplomaService{
		templateSampleRepo: templateSampleRepo,
		universityRepo:     universityRepo,
		majorRepo:          majorRepo,
		facultyRepo:        facultyRepo,
		repo:               repo,
		templateRepo:       templateRepo,
		userRepo:           userRepo,
		minioClient:        minioClient,
		templateEngine:     templateEngine,
		pdfGenerator:       pdfGenerator,
		pqcKeys:            pqcKeys,
		rounds:             rounds,
		notifier:           notifier,
	}
}

// sealAlgorithm returns the parameter set of the university's active signing key,
// which is printed in the PDF stamp. A PDF cannot be rendered without one.
func (s *eDiplomaService) sealAlgorithm(ctx context.Context, universityID primitive.ObjectID) (string, error) {
	key, err := s.pqcKeys.FindActive(ctx, universityID)
	if errors.Is(err, mongo.ErrNoDocuments) || (err == nil && key == nil) {
		return "", ErrNoActivePQCKey
	}
	if err != nil {
		return "", err
	}
	return key.Algorithm, nil
}

func (s *eDiplomaService) GetEDiplomaDTOByID(ctx context.Context, id primitive.ObjectID) (*models.EDiplomaResponse, error) {
	ediploma, err := s.repo.FindByID(ctx, id)
	if err != nil || ediploma == nil {
		return nil, fmt.Errorf("ediploma not found")
	}
	if claims, ok := ctx.Value(utils.ClaimsContextKey).(*utils.CustomClaims); ok {
		if claims.UniversityID != ediploma.UniversityID.Hex() || (claims.Role == "student" && claims.UserID != ediploma.UserID.Hex()) {
			return nil, fmt.Errorf("access denied")
		}
	}

	university, _ := s.universityRepo.FindByID(ctx, ediploma.UniversityID)
	faculty, _ := s.facultyRepo.FindByID(ctx, ediploma.FacultyID)
	template, _ := s.templateRepo.GetByID(ctx, ediploma.TemplateID)
	user, _ := s.userRepo.GetUserByID(ctx, ediploma.UserID)

	dto := mapper.MapEDiplomaToDTO(ediploma, university, faculty, template, user)
	s.attachRounds(ctx, []*models.EDiploma{ediploma}, []*models.EDiplomaResponse{dto})
	return dto, nil
}

func (s *eDiplomaService) GetByID(ctx context.Context, id string) (*models.EDiploma, error) {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, errors.New("invalid diploma id")
	}
	return s.repo.FindByID(ctx, objID)
}

func (s *eDiplomaService) CreateEDiploma(ctx context.Context, claims *utils.CustomClaims, req *models.CreateEDiplomaRequest) (*models.EDiploma, error) {
	universityID, err := primitive.ObjectIDFromHex(claims.UniversityID)
	if err != nil {
		return nil, common.ErrInvalidToken
	}

	if strings.TrimSpace(req.StudentCode) == "" || strings.TrimSpace(req.Name) == "" || req.IssueDate.IsZero() ||
		strings.TrimSpace(req.SerialNumber) == "" || strings.TrimSpace(req.RegistrationNumber) == "" {
		return nil, common.ErrMissingRequiredFieldsForEDiploma
	}

	// Every diploma belongs to an issuance round of the same university.
	if req.RoundID.IsZero() {
		return nil, ErrRoundRequired
	}
	if _, err := s.RequireRound(ctx, universityID, req.RoundID.Hex()); err != nil {
		return nil, err
	}

	user, err := s.userRepo.FindByStudentCodeAndUniversityID(ctx, strings.TrimSpace(req.StudentCode), universityID)
	if err != nil || user == nil {
		return nil, common.ErrUserNotExisted
	}

	existed, err := s.repo.FindByDynamicFilter(ctx, bson.M{
		"university_id":       universityID,
		"serial_number":       strings.TrimSpace(req.SerialNumber),
		"registration_number": strings.TrimSpace(req.RegistrationNumber),
	})
	if err != nil {
		return nil, err
	}
	if len(existed) > 0 {
		return nil, common.ErrEDiplomaAlreadyExists
	}

	now := time.Now()
	ediploma := &models.EDiploma{
		ID:                 primitive.NewObjectID(),
		Name:               strings.TrimSpace(req.Name),
		UniversityID:       universityID,
		FacultyID:          user.FacultyID,
		UserID:             user.ID,
		StudentCode:        strings.TrimSpace(req.StudentCode),
		FullName:           user.FullName,
		CertificateType:    strings.TrimSpace(req.CertificateType),
		Course:             strings.TrimSpace(req.Course),
		EducationType:      strings.TrimSpace(req.EducationType),
		GPA:                req.GPA,
		GraduationRank:     strings.TrimSpace(req.GraduationRank),
		IssueDate:          req.IssueDate,
		SerialNumber:       strings.TrimSpace(req.SerialNumber),
		RegistrationNumber: strings.TrimSpace(req.RegistrationNumber),
		Issued:             false,
		Signed:             false,
		SignedAt:           time.Time{},
		DataEncrypted:      false,
		OnBlockchain:       false,
		Description:        strings.TrimSpace(req.Description),
		RoundID:            req.RoundID,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	if err := s.repo.Save(ctx, ediploma); err != nil {
		return nil, err
	}

	return ediploma, nil
}

func (s *eDiplomaService) GenerateEDiploma(ctx context.Context, ediplomaIDStr, templateIDStr string) (*models.EDiploma, error) {
	ediplomaID, err := primitive.ObjectIDFromHex(ediplomaIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid eDiploma ID")
	}
	templateID, err := primitive.ObjectIDFromHex(templateIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid template ID")
	}

	ediploma, err := s.repo.FindByID(ctx, ediplomaID)
	if err != nil || ediploma == nil {
		return nil, fmt.Errorf("ediploma not found")
	}
	if err := canIssueDiploma(ctx, ediploma); err != nil {
		return nil, err
	}

	template, err := s.templateRepo.GetByID(ctx, templateID)
	if err != nil {
		return nil, fmt.Errorf("template not found")
	}
	if template.UniversityID != ediploma.UniversityID || template.FacultyID != ediploma.FacultyID {
		return nil, fmt.Errorf("template does not belong to the diploma faculty")
	}

	// Lấy TemplateSample từ Template
	sample, err := s.templateSampleRepo.GetByID(ctx, template.TemplateSampleID)
	if err != nil {
		return nil, fmt.Errorf("template sample not found")
	}

	// Kiểm tra hash HTMLContent
	calculatedHash := utils.ComputeSHA256([]byte(sample.HTMLContent))
	if calculatedHash != template.HashTemplate {
		return nil, fmt.Errorf("template content hash mismatch - data may be corrupted")
	}

	user, err := s.userRepo.GetUserByID(ctx, ediploma.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	university, err := s.universityRepo.FindByID(ctx, ediploma.UniversityID)
	if err != nil {
		return nil, fmt.Errorf("failed to get university: %w", err)
	}

	faculty, err := s.facultyRepo.FindByID(ctx, ediploma.FacultyID)
	if err != nil {
		return nil, fmt.Errorf("failed to get faculty: %w", err)
	}

	algorithm, err := s.sealAlgorithm(ctx, ediploma.UniversityID)
	if err != nil {
		return nil, err
	}

	data := diplomaRenderData(ediploma, user, university, faculty, ediploma.IssueDate, algorithm)

	// Render PDF từ sample HTML
	renderedHTML, err := s.templateEngine.Render(sample.HTMLContent, data)
	if err != nil {
		return nil, fmt.Errorf("failed to render HTML: %w", err)
	}

	pdfBytes, err := s.pdfGenerator.ConvertHTMLToPDF(renderedHTML)
	if err != nil {
		return nil, fmt.Errorf("failed to generate PDF: %w", err)
	}

	hash := utils.ComputeSHA256(pdfBytes)
	pdfPath := fmt.Sprintf("ediplomas/%s.pdf", primitive.NewObjectID().Hex())
	if err := s.minioClient.UploadFile(ctx, pdfPath, pdfBytes, "application/pdf"); err != nil {
		return nil, fmt.Errorf("failed to upload PDF: %w", err)
	}

	if err := s.templateRepo.LockTemplate(ctx, templateID); err != nil {
		log.Printf("Failed to lock template: %v", err)
	}

	now := time.Now()
	updates := bson.M{
		"template_id":         templateID,
		"ediploma_file_link":  pdfPath,
		"ediploma_file_hash":  hash,
		"issued":              true,
		"seal_algorithm":      algorithm,
		"signature_of_uni":    template.SignatureOfUni,
		"signature_of_minedu": template.SignatureOfMinEdu,
		"updated_at":          now,
	}
	if err := s.repo.UpdateFields(ctx, ediploma.ID, updates); err != nil {
		return nil, fmt.Errorf("failed to update EDiploma: %w", err)
	}
	if err := s.markStudentGraduated(ctx, ediploma.UserID); err != nil {
		return nil, fmt.Errorf("failed to update student graduation status: %w", err)
	}

	updated, err := s.repo.FindByID(ctx, ediploma.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load updated EDiploma: %w", err)
	}

	return updated, nil
}

func (s *eDiplomaService) SearchEDiplomaDTOs(ctx context.Context, filter models.EDiplomaSearchFilter) ([]*models.EDiplomaResponse, int64, error) {
	ediplomas, total, err := s.repo.SearchByFilters(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	dtoList := make([]*models.EDiplomaResponse, 0, len(ediplomas))
	for _, ed := range ediplomas {
		university, _ := s.universityRepo.FindByID(ctx, ed.UniversityID)
		faculty, _ := s.facultyRepo.FindByID(ctx, ed.FacultyID)

		template, _ := s.templateRepo.GetByID(ctx, ed.TemplateID)
		user, _ := s.userRepo.GetUserByID(ctx, ed.UserID)

		dto := mapper.MapEDiplomaToDTO(ed, university, faculty, template, user)
		dtoList = append(dtoList, dto)
	}
	s.attachRounds(ctx, ediplomas, dtoList)

	return dtoList, total, nil
}

func (s *eDiplomaService) markStudentGraduated(ctx context.Context, userID primitive.ObjectID) error {
	return s.userRepo.UpdateUser(ctx, userID, bson.M{
		"status":     1,
		"updated_at": time.Now(),
	})
}

// service/eDiplomaService.go
func (s *eDiplomaService) GetEDiplomaFile(ctx context.Context, ediplomaID, universityID primitive.ObjectID) (io.ReadCloser, string, error) {
	// Lấy bản ghi EDiploma
	ediploma, err := s.repo.FindByID(ctx, ediplomaID)
	if err != nil || ediploma == nil {
		return nil, "", fmt.Errorf("EDiploma not found")
	}

	// Kiểm tra quyền truy cập theo university
	if ediploma.UniversityID != universityID {
		return nil, "", fmt.Errorf("access denied")
	}
	if claims, ok := ctx.Value(utils.ClaimsContextKey).(*utils.CustomClaims); ok && claims.Role == "student" && claims.UserID != ediploma.UserID.Hex() {
		return nil, "", fmt.Errorf("access denied")
	}

	// Lấy thông tin university để lấy mã trường
	university, err := s.universityRepo.FindByID(ctx, universityID)
	if err != nil || university == nil {
		return nil, "", fmt.Errorf("university not found")
	}

	stream, contentType, err := s.downloadEDiplomaFileWithFallback(ctx, ediploma, university.UniversityCode)
	if err != nil {
		return nil, "", err
	}

	return stream, contentType, nil
}

func (s *eDiplomaService) GetEDiplomaFileByUniversityCode(ctx context.Context, ediplomaID primitive.ObjectID, universityCode string) (io.ReadCloser, string, error) {
	ediploma, err := s.repo.FindByID(ctx, ediplomaID)
	if err != nil || ediploma == nil {
		return nil, "", fmt.Errorf("EDiploma not found")
	}

	university, err := s.universityRepo.FindByCode(ctx, universityCode)
	if err != nil || university == nil {
		return nil, "", fmt.Errorf("university not found")
	}

	if ediploma.UniversityID != university.ID {
		return nil, "", fmt.Errorf("EDiploma does not belong to university")
	}

	stream, contentType, err := s.downloadEDiplomaFileWithFallback(ctx, ediploma, university.UniversityCode)
	if err != nil {
		return nil, "", err
	}

	return stream, contentType, nil
}

func (s *eDiplomaService) downloadEDiplomaFileWithFallback(ctx context.Context, ediploma *models.EDiploma, universityCode string) (io.ReadCloser, string, error) {
	candidates := buildEDiplomaObjectCandidates(ediploma, universityCode)
	var lastErr error

	for _, objectKey := range candidates {
		stream, contentType, err := s.minioClient.DownloadFileStream(ctx, objectKey)
		if err == nil {
			return stream, contentType, nil
		}
		lastErr = err
	}

	if lastErr == nil {
		lastErr = errors.New("no candidate object key")
	}

	return nil, "", fmt.Errorf("failed to get file from MinIO: %w", lastErr)
}

func buildEDiplomaObjectCandidates(ediploma *models.EDiploma, universityCode string) []string {
	// Prefer persisted link in DB, then try legacy key formats.
	rawLink := strings.TrimSpace(ediploma.EDiplomaFileLink)
	legacyPlain := fmt.Sprintf("ediplomas/%s/%s.pdf", universityCode, ediploma.StudentCode)
	legacySigned := fmt.Sprintf("ediplomas/%s/%s_signed.pdf", universityCode, ediploma.StudentCode)

	seen := make(map[string]struct{}, 3)
	add := func(keys *[]string, key string) {
		if key == "" {
			return
		}
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		*keys = append(*keys, key)
	}

	keys := make([]string, 0, 3)
	add(&keys, rawLink)
	add(&keys, legacyPlain)
	add(&keys, legacySigned)

	return keys
}
func (s *eDiplomaService) GetSimpleEDiplomasByUserID(
	ctx context.Context,
	userID primitive.ObjectID,
) ([]*models.EDiplomaSimpleResponse, error) {

	ediplomas, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Students only see diplomas that were actually issued, not drafts.
	responses := []*models.EDiplomaSimpleResponse{}
	for _, ed := range ediplomas {
		if !ed.Issued {
			continue
		}
		responses = append(responses, &models.EDiplomaSimpleResponse{
			ID:                 ed.ID.Hex(),
			Name:               ed.Name,
			Revoked:            ed.Revoked,
			OnBlockchainVerify: ed.OnBlockchainVerify,
		})
	}

	return responses, nil
}
