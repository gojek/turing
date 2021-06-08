package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/gojek/turing/api/turing/models"
	"github.com/jinzhu/gorm"
)

const (
	sparkHomeFolder string = "/home/spark"
)

// EnsemblingJobFindByIDOptions contains the options allowed when finding ensembling jobs.
type EnsemblingJobFindByIDOptions struct {
	ProjectID *models.ID
}

// EnsemblingJobListOptions holds query parameters for EnsemblersService.List method.
type EnsemblingJobListOptions struct {
	PaginationOptions
	ProjectID          *models.ID      `schema:"project_id" validate:"required"`
	Statuses           []models.Status `validate:"oneof=pending running terminated completed failed failed_submission failed_building"` //nolint
	IsLocked           *bool
	RetryCountLessThan *int
	UpdatedAtBefore    *time.Time
}

// EnsemblingJobService is the data access object for the EnsemblingJob from the db.
type EnsemblingJobService interface {
	Save(ensembler *models.EnsemblingJob) error
	FindByID(id models.ID, options EnsemblingJobFindByIDOptions) (*models.EnsemblingJob, error)
	List(options EnsemblingJobListOptions) (*PaginatedResults, error)
	CreateEnsemblingJob(
		job *models.EnsemblingJob,
		projectID models.ID,
		ensembler *models.PyFuncEnsembler,
	) (*models.EnsemblingJob, error)
}

// NewEnsemblingJobService creates a new ensembling job service
func NewEnsemblingJobService(db *gorm.DB, defaultEnvironment string) EnsemblingJobService {
	return &ensemblingJobService{
		db:                 db,
		defaultEnvironment: defaultEnvironment,
	}
}

type ensemblingJobService struct {
	db                 *gorm.DB
	defaultEnvironment string
}

// Save the given router to the db. Updates the existing record if already exists
func (s *ensemblingJobService) Save(ensemblingJob *models.EnsemblingJob) error {
	return s.db.Save(ensemblingJob).Error
}

func (s *ensemblingJobService) FindByID(
	id models.ID,
	options EnsemblingJobFindByIDOptions,
) (*models.EnsemblingJob, error) {
	query := s.db.Where("id = ?", id)

	if options.ProjectID != nil {
		query = query.Where("project_id = ?", options.ProjectID)
	}

	var ensemblingJob models.EnsemblingJob
	result := query.First(&ensemblingJob)

	if err := result.Error; err != nil {
		return &ensemblingJob, err
	}

	return &ensemblingJob, nil
}

func (s *ensemblingJobService) List(options EnsemblingJobListOptions) (*PaginatedResults, error) {
	var results []*models.EnsemblingJob
	var count int
	done := make(chan bool, 1)

	query := s.db
	if options.ProjectID != nil {
		query = query.Where("project_id = ?", options.ProjectID)
	}

	if options.Statuses != nil {
		query = query.Where("status IN (?)", options.Statuses)
	}

	if options.IsLocked != nil {
		query = query.Where("is_locked = ?", options.IsLocked)
	}

	if options.RetryCountLessThan != nil {
		query = query.Where("retry_count < ?", options.RetryCountLessThan)
	}

	if options.UpdatedAtBefore != nil {
		query = query.Where("updated_at < ?", options.UpdatedAtBefore)
	}

	go func() {
		query.Model(&results).Count(&count)
		done <- true
	}()

	result := query.
		Scopes(PaginationScope(options.PaginationOptions)).
		Find(&results)
	<-done

	if err := result.Error; err != nil {
		return nil, err
	}

	paginatedResults := createPaginatedResults(options.PaginationOptions, count, results)
	return paginatedResults, nil
}

func generateDefaultJobName(ensemblerName string) string {
	return fmt.Sprintf("%s: %s", ensemblerName, time.Now().Format(time.RFC3339))
}

func getEnsemblerDirectory(ensembler *models.PyFuncEnsembler) string {
	// Ensembler URI will be a local directory
	// Dockerfile will build copy the artifact into the local directory.
	// See engines/batch-ensembler/app.Dockerfile
	splitURI := strings.Split(ensembler.ArtifactURI, "/")
	return fmt.Sprintf(
		"%s/%s",
		sparkHomeFolder,
		splitURI[len(splitURI)-1],
	)
}

// CreateEnsemblingJob creates an ensembling job.
func (s *ensemblingJobService) CreateEnsemblingJob(
	job *models.EnsemblingJob,
	projectID models.ID,
	ensembler *models.PyFuncEnsembler,
) (*models.EnsemblingJob, error) {
	job.ProjectID = projectID
	job.EnvironmentName = s.defaultEnvironment

	// Populate name if the user does not define a name for the job
	if job.Name == "" {
		job.Name = generateDefaultJobName(ensembler.Name)
	}

	job.JobConfig.JobConfig.Spec.Ensembler.Uri = getEnsemblerDirectory(ensembler)
	job.InfraConfig.ArtifactURI = ensembler.ArtifactURI
	job.InfraConfig.EnsemblerName = ensembler.Name

	// Save ensembling job
	err := s.Save(job)
	if err != nil {
		return nil, err
	}

	return job, nil
}
