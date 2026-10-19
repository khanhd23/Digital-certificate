package handlers

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/vnkmasc/Kmasc/app/backend/internal/common"
	"github.com/vnkmasc/Kmasc/app/backend/internal/models"
	"github.com/vnkmasc/Kmasc/app/backend/internal/service"
	"github.com/vnkmasc/Kmasc/app/backend/pkg/database"
	"github.com/vnkmasc/Kmasc/app/backend/utils"
	"github.com/xuri/excelize/v2"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type EDiplomaHandler struct {
	universityService service.UniversityService
	ediplomaService   service.EDiplomaService
	minioClient       *database.MinioClient
}

func NewEDiplomaHandler(
	ediplomaService service.EDiplomaService,
) *EDiplomaHandler {
	return &EDiplomaHandler{
		ediplomaService: ediplomaService,
	}
}

type generateEDiplomaRequest struct {
	EDiplomaID string `json:"ediploma_id" binding:"required"`
	TemplateID string `json:"template_id" binding:"required"`
}

type revokeEDiplomaRequest struct {
	Reason string `json:"reason" binding:"required,min=5,max=500"`
}

type createEDiplomaRequest struct {
	StudentCode        string  `json:"student_code" binding:"required"`
	Name               string  `json:"name" binding:"required"`
	CertificateType    string  `json:"certificate_type"`
	Course             string  `json:"course"`
	EducationType      string  `json:"education_type"`
	GPA                float64 `json:"gpa"`
	GraduationRank     string  `json:"graduation_rank"`
	IssueDate          string  `json:"issue_date" binding:"required"`
	SerialNumber       string  `json:"serial_number" binding:"required"`
	RegistrationNumber string  `json:"registration_number" binding:"required"`
	Description        string  `json:"description"`
	RoundID            string  `json:"round_id"`
}

func (h *EDiplomaHandler) CreateEDiploma(c *gin.Context) {
	claimsRaw, exists := c.Get("claims")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Phiên đăng nhập không hợp lệ, vui lòng đăng nhập lại"})
		return
	}

	claims, ok := claimsRaw.(*utils.CustomClaims)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Phiên đăng nhập không hợp lệ, vui lòng đăng nhập lại"})
		return
	}

	var req createEDiplomaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Dữ liệu không hợp lệ"})
		return
	}

	issueDate, err := parseEDiplomaIssueDate(req.IssueDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ngày cấp không hợp lệ (dd/mm/yyyy hoặc yyyy-mm-dd)"})
		return
	}
	round, ok := h.requireRound(c, claims, req.RoundID)
	if !ok {
		return
	}

	serviceReq := &models.CreateEDiplomaRequest{
		RoundID:            round.ID,
		StudentCode:        req.StudentCode,
		Name:               req.Name,
		CertificateType:    req.CertificateType,
		Course:             req.Course,
		EducationType:      req.EducationType,
		GPA:                req.GPA,
		GraduationRank:     req.GraduationRank,
		IssueDate:          issueDate,
		SerialNumber:       req.SerialNumber,
		RegistrationNumber: req.RegistrationNumber,
		Description:        req.Description,
	}

	ediploma, err := h.ediplomaService.CreateEDiploma(c.Request.Context(), claims, serviceReq)
	if err != nil {
		switch err {
		case common.ErrInvalidToken:
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Phiên đăng nhập không hợp lệ, vui lòng đăng nhập lại"})
		case common.ErrEDiplomaAlreadyExists:
			c.JSON(http.StatusConflict, gin.H{"error": "Số hiệu và số vào sổ đã tồn tại"})
		case common.ErrUserNotExisted:
			c.JSON(http.StatusNotFound, gin.H{"error": "Không tìm thấy sinh viên"})
		case common.ErrMissingRequiredFieldsForEDiploma:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Thiếu thông tin bắt buộc để tạo văn bằng"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": ediploma})
}

func (h *EDiplomaHandler) ImportEDiplomasFromExcel(c *gin.Context) {
	val, exists := c.Get("claims")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Phiên đăng nhập không hợp lệ, vui lòng đăng nhập lại"})
		return
	}
	claims, ok := val.(*utils.CustomClaims)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Phiên đăng nhập không hợp lệ, vui lòng đăng nhập lại"})
		return
	}

	// The whole list is imported into one issuance round.
	round, ok := h.requireRound(c, claims, c.PostForm("round_id"))
	if !ok {
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Vui lòng chọn tệp Excel"})
		return
	}

	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Không thể mở tệp"})
		return
	}
	defer src.Close()

	f, err := excelize.OpenReader(src)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Tệp không đúng định dạng Excel"})
		return
	}

	rows, err := f.GetRows("Sheet1")
	if err != nil || len(rows) <= 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Không tìm thấy dữ liệu trong trang tính đầu tiên"})
		return
	}

	// Pre-allocate slices để tránh re-allocations
	estimatedCapacity := len(rows) - 1 // Exclude header row
	successResults := make([]map[string]interface{}, 0, estimatedCapacity)
	errorResults := make([]map[string]interface{}, 0, estimatedCapacity)

	for i, row := range rows {
		if i == 0 {
			continue
		}

		result := map[string]interface{}{"row": i + 1}
		get := func(index int) string {
			if index < len(row) {
				return strings.TrimSpace(row[index])
			}
			return ""
		}

		issueDate, err := parseEDiplomaIssueDate(get(11))
		if err != nil {
			result["error"] = "Ngày cấp không hợp lệ"
			errorResults = append(errorResults, result)
			continue
		}

		gpa := 0.0
		if gpaStr := get(10); gpaStr != "" {
			gpa, _ = strconv.ParseFloat(gpaStr, 64)
		}

		serviceReq := &models.CreateEDiplomaRequest{
			RoundID:            round.ID,
			StudentCode:        get(0),
			Name:               get(2),
			CertificateType:    get(3),
			Course:             get(4),
			EducationType:      get(6),
			GPA:                gpa,
			GraduationRank:     get(5),
			IssueDate:          issueDate,
			SerialNumber:       get(7),
			RegistrationNumber: get(8),
			Description:        "",
		}

		ediploma, err := h.ediplomaService.CreateEDiploma(c.Request.Context(), claims, serviceReq)
		if err != nil {
			result["error"] = mapEDiplomaImportErrorToMessage(err)
			errorResults = append(errorResults, result)
			continue
		}

		result["status"] = "Tạo thành công"
		result["ediploma_id"] = ediploma.ID.Hex()
		result["student_code"] = serviceReq.StudentCode
		successResults = append(successResults, result)
	}

	if len(errorResults) == 0 {
		c.JSON(http.StatusCreated, gin.H{
			"message":       "Đã thêm toàn bộ văn bằng",
			"success_count": len(successResults),
			"error_count":   0,
			"data":          gin.H{"success": successResults},
		})
		return
	}

	c.JSON(http.StatusMultiStatus, gin.H{
		"message":       "Một số dòng không thêm được",
		"success_count": len(successResults),
		"error_count":   len(errorResults),
		"data": gin.H{
			"success": successResults,
			"error":   errorResults,
		},
	})
}

func mapEDiplomaImportErrorToMessage(err error) string {
	switch {
	case errors.Is(err, common.ErrInvalidToken):
		return "Token không hợp lệ"
	case errors.Is(err, common.ErrEDiplomaAlreadyExists):
		return "Số hiệu và số vào sổ đã tồn tại"
	case errors.Is(err, common.ErrUserNotExisted):
		return "Không tìm thấy sinh viên"
	case errors.Is(err, common.ErrMissingRequiredFieldsForEDiploma):
		return "Thiếu thông tin bắt buộc"
	default:
		return "Lỗi hệ thống hoặc không xác định"
	}
}

func parseEDiplomaIssueDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}

	if t, err := time.Parse("02/01/2006", s); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02", s)
}

// Handler
func (h *EDiplomaHandler) SearchEDiplomas(c *gin.Context) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page <= 0 {
		page = 1
	}

	pageSize, err := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	if err != nil || pageSize <= 0 {
		pageSize = 10
	}

	var issued *bool
	if issuedStr := c.Query("issued"); issuedStr != "" {
		v, err := strconv.ParseBool(issuedStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Bộ lọc trạng thái cấp không hợp lệ"})
			return
		}
		issued = &v
	}

	var revoked *bool
	if revokedStr := c.Query("revoked"); revokedStr != "" {
		v, err := strconv.ParseBool(revokedStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Bộ lọc trạng thái thu hồi không hợp lệ"})
			return
		}
		revoked = &v
	}

	claims, ok := c.MustGet("claims").(*utils.CustomClaims)
	if !ok || claims.UniversityID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Phiên đăng nhập không hợp lệ, vui lòng đăng nhập lại"})
		return
	}

	// The university always comes from the token, never from the query string.
	filters := models.EDiplomaSearchFilter{
		UniversityID:    claims.UniversityID,
		FacultyID:       strings.TrimSpace(c.Query("faculty_id")),
		CertificateType: strings.TrimSpace(c.Query("certificate_type")),
		Course:          strings.TrimSpace(c.Query("course")),
		Issued:          issued,
		Revoked:         revoked,
		RoundID:         strings.TrimSpace(c.Query("round_id")),
		Keyword:         strings.TrimSpace(c.Query("keyword")),
		Page:            page,
		PageSize:        pageSize,
	}

	dtoList, total, err := h.ediplomaService.SearchEDiplomaDTOs(c.Request.Context(), filters)
	if err != nil {
		if err.Error() == "invalid round_id" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Đợt cấp không hợp lệ"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	totalPage := int(math.Ceil(float64(total) / float64(pageSize)))

	c.JSON(http.StatusOK, gin.H{
		"data":       dtoList,
		"page":       page,
		"page_size":  pageSize,
		"total":      total,
		"total_page": totalPage,
	})
}

func (h *EDiplomaHandler) GetMyEDiplomaNames(c *gin.Context) {
	val, exists := c.Get(string(utils.ClaimsContextKey))
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Phiên đăng nhập không hợp lệ, vui lòng đăng nhập lại"})
		return
	}

	claims, ok := val.(*utils.CustomClaims)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Phiên đăng nhập không hợp lệ, vui lòng đăng nhập lại"})
		return
	}

	userID, err := primitive.ObjectIDFromHex(claims.UserID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Phiên đăng nhập không hợp lệ, vui lòng đăng nhập lại"})
		return
	}

	ediplomas, err := h.ediplomaService.GetSimpleEDiplomasByUserID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Lỗi hệ thống"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": ediplomas})
}

func (h *EDiplomaHandler) GenerateEDiploma(c *gin.Context) {
	var req generateEDiplomaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Dữ liệu không hợp lệ"})
		return
	}

	c.Set("audit_resource", req.EDiplomaID)
	ediploma, err := h.ediplomaService.GenerateEDiploma(
		c.Request.Context(),
		req.EDiplomaID,
		req.TemplateID,
	)
	if err != nil {
		c.JSON(generationStatus(err), gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, ediploma)
}

// generationStatus reports a missing signing key as a conflict the school can
// fix (create and activate a key), not as a server failure.
func generationStatus(err error) int {
	if errors.Is(err, service.ErrNoActivePQCKey) {
		return http.StatusConflict
	}
	return http.StatusInternalServerError
}

func (h *EDiplomaHandler) GetEDiplomaByID(c *gin.Context) {
	ctx := c.Request.Context()
	ediplomaIDHex := c.Param("id")
	ediplomaID, err := primitive.ObjectIDFromHex(ediplomaIDHex)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Mã văn bằng không hợp lệ"})
		return
	}

	dto, err := h.ediplomaService.GetEDiplomaDTOByID(ctx, ediplomaID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Không tìm thấy văn bằng"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": dto})
}

func (h *EDiplomaHandler) RevokeEDiploma(c *gin.Context) {
	claimsRaw, exists := c.Get("claims")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Phiên đăng nhập không hợp lệ, vui lòng đăng nhập lại"})
		return
	}
	claims, ok := claimsRaw.(*utils.CustomClaims)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Phiên đăng nhập không hợp lệ, vui lòng đăng nhập lại"})
		return
	}
	universityID, err := primitive.ObjectIDFromHex(claims.UniversityID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Phiên đăng nhập không hợp lệ, vui lòng đăng nhập lại"})
		return
	}
	diplomaID, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Mã văn bằng không hợp lệ"})
		return
	}
	var req revokeEDiplomaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Lý do thu hồi cần từ 5 đến 500 ký tự"})
		return
	}
	if err := h.ediplomaService.RevokeEDiploma(c.Request.Context(), universityID, diplomaID, req.Reason); err != nil {
		status := http.StatusInternalServerError
		if err.Error() == "ediploma not found" {
			status = http.StatusNotFound
		}
		if err.Error() == "access denied" {
			status = http.StatusForbidden
		}
		if err.Error() == "ediploma is already revoked" || err.Error() == "only issued diplomas can be revoked" {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Đã thu hồi văn bằng"})
}

func (h *EDiplomaHandler) ViewEDiplomaFile(c *gin.Context) {
	// Lấy claims từ token
	claimsRaw, exists := c.Get("claims")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Phiên đăng nhập không hợp lệ, vui lòng đăng nhập lại"})
		return
	}
	claims, ok := claimsRaw.(*utils.CustomClaims)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Phiên đăng nhập không hợp lệ, vui lòng đăng nhập lại"})
		return
	}
	universityID, _ := primitive.ObjectIDFromHex(claims.UniversityID)

	// Lấy ediplomaID từ URL
	ediplomaIDHex := c.Param("id")
	ediplomaID, err := primitive.ObjectIDFromHex(ediplomaIDHex)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Mã văn bằng không hợp lệ"})
		return
	}

	// Lấy stream và content type từ service
	stream, contentType, err := h.ediplomaService.GetEDiplomaFile(c.Request.Context(), ediplomaID, universityID)
	if err != nil {
		if err.Error() == "EDiploma not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if err.Error() == "access denied" {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer stream.Close()

	// Trả file về client
	c.DataFromReader(http.StatusOK, -1, contentType, stream, nil)
}

func (h *EDiplomaHandler) PublicViewEDiplomaFile(c *gin.Context) {
	// Lấy params từ URL hoặc query
	universityCode := c.Query("university_code")
	if universityCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Thiếu mã trường"})
		return
	}

	ediplomaIDHex := c.Query("ediploma_id")
	if ediplomaIDHex == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Thiếu mã văn bằng"})
		return
	}
	ediplomaID, err := primitive.ObjectIDFromHex(ediplomaIDHex)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Mã văn bằng không hợp lệ"})
		return
	}

	// Lấy stream và content type từ service
	stream, contentType, err := h.ediplomaService.GetEDiplomaFileByUniversityCode(c.Request.Context(), ediplomaID, universityCode)
	if err != nil {
		if err.Error() == "EDiploma not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if err.Error() == "university not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer stream.Close()

	// Trả file về client
	c.DataFromReader(http.StatusOK, -1, contentType, stream, nil)
}
