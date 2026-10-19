package handlers

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/vnkmasc/Kmasc/app/backend/internal/models"
	"github.com/vnkmasc/Kmasc/app/backend/internal/service"
	"github.com/vnkmasc/Kmasc/app/backend/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"net/http"
	"strings"
	"time"
)

// DegreePortal exposes a deliberately small public projection, never student contact data or private keys.
type DegreePortal struct {
	DB      *mongo.Database
	PQC     service.PQCService
	Degrees service.EDiplomaService
}

func (h *DegreePortal) IssueBatch(c *gin.Context) {
	university, ok := universityAdminID(c)
	if !ok {
		return
	}
	var req struct {
		Faculty  string   `json:"faculty_id"`
		Round    string   `json:"round_id"`
		Template string   `json:"template_id"`
		Course   string   `json:"course"`
		Type     string   `json:"certificate_type"`
		Stage    string   `json:"stage"`
		IDs      []string `json:"ids"`
		DryRun   bool     `json:"dry_run"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "Dữ liệu không hợp lệ"})
		return
	}
	// stage "issue": unissued records are issued, then signed.
	// stage "sign": issued records that were never signed are only signed.
	// No stage covers both, for older clients.
	if req.Stage != "" && req.Stage != "issue" && req.Stage != "sign" {
		c.JSON(400, gin.H{"error": "Giai đoạn không hợp lệ"})
		return
	}
	issuing := req.Stage != "sign"
	// Issuing needs one faculty: the diploma template belongs to a faculty.
	if _, err := primitive.ObjectIDFromHex(req.Faculty); issuing && err != nil {
		writeRoundError(c, service.ErrFacultyRequired)
		return
	}
	filter, err := h.Degrees.RoundScope(c.Request.Context(), university, req.Faculty, req.Round)
	if err != nil {
		writeRoundError(c, err)
		return
	}
	if _, err = primitive.ObjectIDFromHex(req.Template); issuing && !req.DryRun && err != nil {
		c.JSON(400, gin.H{"error": "Chọn mẫu bằng"})
		return
	}
	unissued := bson.M{"issued": bson.M{"$ne": true}}
	unsigned := bson.M{"issued": true, "pqc_proof": bson.M{"$exists": false}, "on_blockchain": bson.M{"$ne": true}}
	switch req.Stage {
	case "issue":
		filter["issued"] = unissued["issued"]
	case "sign":
		for k, v := range unsigned {
			filter[k] = v
		}
	default:
		filter["$or"] = bson.A{unissued, unsigned}
	}
	filter["revoked"] = bson.M{"$ne": true}
	if req.Course != "" {
		filter["course"] = req.Course
	}
	if req.Type != "" {
		filter["certificate_type"] = req.Type
	}
	// The client may process the scope a few records at a time to show progress;
	// the ids only narrow the scope, they never widen it.
	if len(req.IDs) > 0 {
		ids := make([]primitive.ObjectID, 0, len(req.IDs))
		for _, raw := range req.IDs {
			id, err := primitive.ObjectIDFromHex(raw)
			if err != nil {
				c.JSON(400, gin.H{"error": "Mã văn bằng không hợp lệ"})
				return
			}
			ids = append(ids, id)
		}
		filter["_id"] = bson.M{"$in": ids}
	}
	limit := 100
	if req.DryRun {
		limit = 1000
	}
	cursor, err := h.DB.Collection("ediplomas").Find(c, filter,
		options.Find().SetLimit(int64(limit+1)).SetSort(bson.D{{Key: "student_code", Value: 1}}).
			SetProjection(bson.M{"student_code": 1, "issued": 1}))
	if err != nil {
		c.JSON(503, gin.H{"error": "Không thể tải danh sách"})
		return
	}
	defer cursor.Close(c)
	docs := []models.EDiploma{}
	if cursor.All(c, &docs) != nil {
		c.JSON(503, gin.H{"error": "Không thể đọc danh sách"})
		return
	}
	if len(docs) > limit {
		c.JSON(400, gin.H{"error": fmt.Sprintf("Tối đa %d văn bằng mỗi lần. Thu hẹp bộ lọc khóa học.", limit)})
		return
	}
	if req.DryRun {
		targets := make([]gin.H, 0, len(docs))
		for _, d := range docs {
			targets = append(targets, gin.H{"id": d.ID.Hex(), "student_code": d.StudentCode, "issued": d.Issued})
		}
		c.JSON(200, gin.H{"data": targets, "total": len(targets)})
		return
	}
	results := []gin.H{}
	for _, d := range docs {
		row := gin.H{"id": d.ID.Hex(), "student_code": d.StudentCode, "issued": d.Issued, "signed": false}
		if !d.Issued {
			if _, err = h.Degrees.GenerateEDiploma(c.Request.Context(), d.ID.Hex(), req.Template); err != nil {
				row["error"] = err.Error()
				results = append(results, row)
				continue
			}
			row["issued"] = true
		}
		if _, err = h.PQC.SignEDiploma(c.Request.Context(), university, d.ID); err != nil {
			row["error"] = err.Error()
		} else {
			row["signed"] = true
		}
		results = append(results, row)
	}
	c.JSON(200, gin.H{"data": results})
}

func (h *DegreePortal) Public(c *gin.Context) {
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "Mã văn bằng không hợp lệ"})
		return
	}
	var d models.EDiploma
	if err = h.DB.Collection("ediplomas").FindOne(c, bson.M{"_id": id, "issued": true}).Decode(&d); err != nil {
		c.JSON(404, gin.H{"error": "Không tìm thấy văn bằng đã cấp"})
		return
	}
	var u models.University
	_ = h.DB.Collection("universities").FindOne(c, bson.M{"_id": d.UniversityID}).Decode(&u)
	result, err := h.PQC.VerifyEDiploma(c.Request.Context(), id)
	if err != nil {
		c.JSON(503, gin.H{"error": "Chưa thể xác minh. Vui lòng thử lại."})
		return
	}
	revoked := d.Revoked || result.Blockchain.RevokedOnChain
	c.Set("audit_university", d.UniversityID.Hex())
	c.Set("audit_verified", result.Valid && !revoked)
	c.JSON(200, gin.H{"data": gin.H{"id": d.ID, "full_name": d.FullName, "university_name": u.UniversityName, "name": d.Name, "certificate_type": d.CertificateType, "issue_date": d.IssueDate, "serial_number": d.SerialNumber, "revoked": revoked, "revoked_at": d.RevokedAt, "revocation_on_chain": result.Blockchain.RevokedOnChain, "file_hash": d.EDiplomaFileHash, "verification": result}})
}

func (h *DegreePortal) Summary(c *gin.Context) {
	university, ok := universityAdminID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	count := func(collection string, extra bson.M) (int64, error) {
		extra["university_id"] = university
		return h.DB.Collection(collection).CountDocuments(ctx, extra)
	}
	stats := gin.H{}
	for name, filter := range map[string]bson.M{"total": {}, "issued": {"issued": true}, "signed": {"signed": true}, "anchored": {"on_blockchain": true}, "revoked": {"revoked": true}} {
		n, err := count("ediplomas", filter)
		if err != nil {
			c.JSON(503, gin.H{"error": "Không thể tải thống kê"})
			return
		}
		stats[name] = n
	}
	n, err := count("users", bson.M{})
	if err != nil {
		c.JSON(503, gin.H{"error": "Không thể tải thống kê sinh viên"})
		return
	}
	stats["students"] = n
	attempts, err := h.DB.Collection("audit_events").CountDocuments(ctx, bson.M{"university_id": university.Hex(), "verification_valid": bson.M{"$exists": true}})
	if err != nil {
		c.JSON(503, gin.H{"error": "Không thể tải thống kê xác minh"})
		return
	}
	verified, err := h.DB.Collection("audit_events").CountDocuments(ctx, bson.M{"university_id": university.Hex(), "verification_valid": true})
	if err != nil {
		c.JSON(503, gin.H{"error": "Không thể tải thống kê xác minh"})
		return
	}
	stats["verification_attempts"] = attempts
	stats["verification_passed"] = verified
	c.JSON(200, gin.H{"data": stats})
}

func (h *DegreePortal) AuditList(c *gin.Context) {
	university, ok := universityAdminID(c)
	if !ok {
		return
	}
	filter := bson.M{"university_id": university.Hex(), "role": bson.M{"$ne": "admin"}}
	filter["verification_valid"] = bson.M{"$exists": false}
	if id := c.Query("diploma_id"); id != "" {
		oid, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			c.JSON(400, gin.H{"error": "Mã văn bằng không hợp lệ"})
			return
		}
		var d models.EDiploma
		if h.DB.Collection("ediplomas").FindOne(c, bson.M{"_id": oid, "university_id": university}).Decode(&d) != nil {
			c.JSON(404, gin.H{"error": "Không tìm thấy văn bằng"})
			return
		}
		events := []bson.M{}
		for i := len(d.History) - 1; i >= 0 && len(events) < 100; i-- {
			events = append(events, d.History[i])
		}
		c.JSON(200, gin.H{"data": events})
		return
	}
	cursor, err := h.DB.Collection("audit_events").Find(c, filter, options.Find().SetSort(bson.D{{Key: "at", Value: -1}}).SetLimit(100))
	if err != nil {
		c.JSON(503, gin.H{"error": "Không thể tải nhật ký"})
		return
	}
	defer cursor.Close(c)
	events := []bson.M{}
	if err = cursor.All(c, &events); err != nil {
		c.JSON(503, gin.H{"error": "Không thể đọc nhật ký"})
		return
	}
	c.JSON(200, gin.H{"data": events})
}

// Audit records operation metadata only, never request bodies, tokens, or private keys.
func (h *DegreePortal) Audit() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		path := c.FullPath()
		if path == "" || c.Request.Method == "OPTIONS" || strings.Contains(path, "/auth/") {
			return
		}
		if !isAuditedAction(path, c.Request.Method) {
			return
		}
		if c.Request.Method == "GET" && !strings.Contains(path, "/public/degrees/") {
			return
		}
		actor, role, university := "anonymous", "public", ""
		if v, ok := c.Get("claims"); ok {
			if claims, ok := v.(*utils.CustomClaims); ok {
				actor = claims.AccountID
				role = claims.Role
				university = claims.UniversityID
			}
		}
		if v, ok := c.Get("audit_university"); ok {
			university, _ = v.(string)
		}
		if university == "" || university == primitive.NilObjectID.Hex() {
			return
		}
		resource := c.Param("id")
		if v, ok := c.Get("audit_resource"); ok {
			resource, _ = v.(string)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		event := bson.M{"at": time.Now().UTC(), "actor": actor, "role": role, "university_id": university, "resource_id": resource, "method": c.Request.Method, "action": path, "status": c.Writer.Status(), "success": c.Writer.Status() < 400}
		if value, ok := c.Get("audit_verified"); ok {
			event["verification_valid"] = value
		}
		_, err := h.DB.Collection("audit_events").InsertOne(ctx, event)
		if err != nil {
			_ = c.Error(err)
		}
	}
}

func isAuditedAction(path, method string) bool {
	path = strings.TrimPrefix(path, "/api/v1")
	if method == "GET" && strings.Contains(path, "/public/degrees/") {
		return true
	}
	if method != "POST" && method != "PUT" && method != "PATCH" {
		return false
	}
	for _, prefix := range []string{"/ediplomas", "/pqc/keys", "/pqc/ediplomas", "/blockchain/push-ediploma", "/blockchain/push-revocations", "/issuance-rounds", "/universities"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func (h *DegreePortal) Replace(c *gin.Context) {
	university, ok := universityAdminID(c)
	if !ok {
		return
	}
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "Mã văn bằng không hợp lệ"})
		return
	}
	var req struct {
		Serial       string    `json:"serial_number"`
		Registration string    `json:"registration_number"`
		Date         time.Time `json:"issue_date"`
	}
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.Serial) == "" || strings.TrimSpace(req.Registration) == "" || req.Date.IsZero() {
		c.JSON(400, gin.H{"error": "Nhập số hiệu, số vào sổ và ngày cấp mới"})
		return
	}
	var old models.EDiploma
	if h.DB.Collection("ediplomas").FindOne(c, bson.M{"_id": id, "university_id": university, "revoked": true}).Decode(&old) != nil {
		c.JSON(409, gin.H{"error": "Chỉ tạo bản thay thế cho văn bằng đã thu hồi của trường"})
		return
	}
	// Deterministic ID makes concurrent requests create at most one replacement, without a multi-document transaction.
	digest := utils.ComputeSHA256([]byte("replacement:" + id.Hex()))
	nextID, _ := primitive.ObjectIDFromHex(digest[:24])
	n, err := h.DB.Collection("ediplomas").CountDocuments(c, bson.M{"university_id": university, "$or": []bson.M{{"serial_number": strings.TrimSpace(req.Serial)}, {"registration_number": strings.TrimSpace(req.Registration)}}})
	if err != nil {
		c.JSON(503, gin.H{"error": "Không thể kiểm tra số hiệu"})
		return
	}
	if n > 0 {
		c.JSON(409, gin.H{"error": "Số hiệu hoặc số vào sổ đã tồn tại"})
		return
	}
	next := models.EDiploma{ID: nextID, UniversityID: old.UniversityID, FacultyID: old.FacultyID, UserID: old.UserID, MajorID: old.MajorID, Name: old.Name, FullName: old.FullName, StudentCode: old.StudentCode, CertificateType: old.CertificateType, Course: old.Course, EducationType: old.EducationType, GPA: old.GPA, GraduationRank: old.GraduationRank, SerialNumber: strings.TrimSpace(req.Serial), RegistrationNumber: strings.TrimSpace(req.Registration), IssueDate: req.Date, ReplacesID: id.Hex(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		// The replacement stays in the original's issuance round, so it is re-issued with that round.
		RoundID: old.RoundID}
	claimsRaw, _ := c.Get("claims")
	claims, _ := claimsRaw.(*utils.CustomClaims)
	next.History = []bson.M{{"_id": primitive.NewObjectID(), "at": time.Now().UTC(), "actor": claims.AccountID, "role": claims.Role, "action": "Tạo bản thay thế từ " + id.Hex(), "success": true}}
	if _, err = h.DB.Collection("ediplomas").InsertOne(c, next); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			c.JSON(409, gin.H{"error": "Bản thay thế đã được tạo. Kiểm tra danh sách chưa cấp."})
		} else {
			c.JSON(503, gin.H{"error": "Không thể tạo bản thay thế"})
		}
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": next})
}
