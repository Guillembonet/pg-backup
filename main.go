package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"

	"github.com/spf13/cobra"
)

var (
	pgBackupLastSuccess = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pg_backup_last_success",
			Help: "Unix time when the script is executed",
		},
		[]string{"database"},
	)
	pgBackupLastFailure = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pg_backup_last_failure",
			Help: "Unix time when the script is executed and failed",
		},
		[]string{"database"},
	)
)

const (
	connectionStringFlag = "connection-string"
	scriptPathFlag       = "script-path"
	backupDirFlag        = "backup-dir"
	pushGatewayFlag      = "push-gateway"
	jobFlag              = "job"
	daysOldFlag          = "days-old"
	cronScheduleFlag     = "cron-schedule"

	cronScheduleEnvVar = "CRON_SCHEDULE"
	pushGatewayEnvVar  = "PUSH_GATEWAY"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("failed to execute command")
	}
}

func init() {
	rootCmd.AddCommand(startCmd, backupCmd)
}

var rootCmd = cobra.Command{
	Use:   "pg-backup",
	Short: "pg-backup - a simple CLI to help with PostgreSQL backups",
}

var startCmd = &cobra.Command{
	Use:     "start",
	Aliases: []string{"s"},
	Short:   "Starts the backup process",
	Run: func(cmd *cobra.Command, args []string) {
		prometheus.MustRegister(pgBackupLastSuccess)

		cronSchedule, err := cmd.Flags().GetString(cronScheduleFlag)
		if err != nil {
			log.Fatal().Err(err).Msg("error getting cron schedule")
		}
		if cronSchedule == "" {
			var ok bool
			cronSchedule, ok = os.LookupEnv(cronScheduleEnvVar)
			if !ok {
				log.Warn().Msgf("environment variable %s not set, using default: * * * * *", cronScheduleEnvVar)
				cronSchedule = "* * * * *"
			}
		}

		c := cron.New()
		c.AddFunc(cronSchedule, func() {
			err := doBackups(cmd)
			if err != nil {
				log.Error().Err(err).Msg("error during backup")
			}
		})
		c.Start()

		log.Info().Str("cron_schedule", cronSchedule).Msg("started")
		select {}
	},
}

var backupCmd = &cobra.Command{
	Use:     "backup",
	Aliases: []string{"b"},
	Short:   "Executes the backup script",
	Run: func(cmd *cobra.Command, args []string) {
		err := doBackups(cmd)
		if err != nil {
			log.Fatal().Err(err).Msg("error during backup")
		}
	},
}

func init() {
	cmds := []*cobra.Command{startCmd, backupCmd}
	for _, cmd := range cmds {
		cmd.Flags().StringArrayP(connectionStringFlag, "c", []string{}, "Connection string to the database to backup")
		cmd.Flags().StringP(scriptPathFlag, "s", "/var/scripts/backup.sh", "Path to the script to execute")
		cmd.Flags().StringP(backupDirFlag, "b", "/backups", "Path to the directory where the backups are stored")
		cmd.Flags().StringP(pushGatewayFlag, "p", "", "Push gateway to send metrics to")
		cmd.Flags().StringP(jobFlag, "j", "pg-backup", "Job label")
		cmd.Flags().IntP(daysOldFlag, "d", 7, "Number of days to keep old backups")
		cmd.Flags().StringP(cronScheduleFlag, "r", "", "Cron schedule")
	}
}

func doBackups(cmd *cobra.Command) error {
	connectionStrings, err := cmd.Flags().GetStringArray(connectionStringFlag)
	if err != nil {
		return fmt.Errorf("error getting connection strings: %w", err)
	}
	scriptPath, err := cmd.Flags().GetString(scriptPathFlag)
	if err != nil {
		return fmt.Errorf("error getting script path: %w", err)
	}
	backupDir, err := cmd.Flags().GetString(backupDirFlag)
	if err != nil {
		return fmt.Errorf("error getting backup dir: %w", err)
	}
	pushGateway, err := cmd.Flags().GetString(pushGatewayFlag)
	if err != nil {
		return fmt.Errorf("error getting push gateway: %w", err)
	}
	if pushGateway == "" {
		pushGateway = os.Getenv(pushGatewayEnvVar)
	}
	job, err := cmd.Flags().GetString(jobFlag)
	if err != nil {
		return fmt.Errorf("error getting job: %w", err)
	}
	daysOld, err := cmd.Flags().GetInt(daysOldFlag)
	if err != nil {
		return fmt.Errorf("error getting days old: %w", err)
	}

	log.Info().Int("backups", len(connectionStrings)).
		Str("script_path", scriptPath).Str("backup_dir", backupDir).Msg("starting backups process")

	for _, connectionString := range connectionStrings {
		command := exec.Command(scriptPath)
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr

		tmp := strings.TrimSpace(strings.TrimPrefix(connectionString, "postgres://"))

		user, tmp, ok := strings.Cut(tmp, ":")
		if !ok {
			return fmt.Errorf("invalid connection string: %s", connectionString)
		}
		command.Env = append(command.Env, fmt.Sprintf("PGUSER=%s", user))

		password, tmp, ok := strings.Cut(tmp, "@")
		if !ok {
			return fmt.Errorf("invalid connection string: %s", connectionString)
		}
		command.Env = append(command.Env, fmt.Sprintf("PGPASSWORD=%s", password))

		host, tmp, ok := strings.Cut(tmp, ":")
		if !ok {
			return fmt.Errorf("invalid connection string: %s", connectionString)
		}
		command.Env = append(command.Env, fmt.Sprintf("PGHOST=%s", host))

		port, val, ok := strings.Cut(tmp, "/")
		if !ok {
			return fmt.Errorf("invalid connection string: %s", connectionString)
		}
		command.Env = append(command.Env, fmt.Sprintf("PGPORT=%s", port))

		database, _, _ := strings.Cut(val, "?")
		command.Env = append(command.Env, fmt.Sprintf("PGDATABASE=%s", database))

		command.Env = append(command.Env, fmt.Sprintf("BACKUP_DIR=%s/%s", strings.TrimSuffix(backupDir, "/"), database))
		command.Env = append(command.Env, fmt.Sprintf("DAYS_OLD=%d", daysOld))

		log.Info().Str("database", database).Msg("starting backup")
		err := command.Run()
		if err != nil {
			pgBackupLastFailure.WithLabelValues(database).SetToCurrentTime()
			metricsErr := pushMetrics(pushGateway, job, pgBackupLastFailure)
			if metricsErr != nil {
				log.Error().Err(metricsErr).Msg("error pushing metric")
			}
			return fmt.Errorf("error executing script: %w", err)
		}
		pgBackupLastSuccess.WithLabelValues(database).SetToCurrentTime()
		err = pushMetrics(pushGateway, job, pgBackupLastSuccess)
		if err != nil {
			log.Error().Err(err).Msg("error pushing metric")
		}
		log.Info().Str("database", database).Msg("backup successful")
	}
	return nil
}

func pushMetrics(pushGateway, job string, metric prometheus.Collector) error {
	if pushGateway == "" {
		log.Warn().Msgf("push gateway not set, skipping metrics push")
		return nil
	}
	err := push.New(pushGateway, job).Collector(metric).Push()
	if err != nil {
		return fmt.Errorf("error pushing metric: %w", err)
	}
	return nil
}
