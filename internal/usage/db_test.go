package usage

import (
	"os"
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openmcp-project/controller-utils/pkg/logging"

	k8s "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Database Module", func() {
	BeforeEach(func() {
		os.Setenv("USAGE_DB_PATH", "")
	})
	Context("When getting the Database", func() {
		It("should return a database entity", func() {
			_, err := GetDB()
			Ω(err).Should(Succeed())
		})
	})
	Context("When initializing the database", func() {
		It("should successfully initialize the database correctly", func(ctx SpecContext) {
			log, err := logging.GetLogger()
			Ω(err).Should(Succeed())

			err = InitDB(ctx, &log)
			Ω(err).Should(Succeed())
		})
		It("should have created the tables correctly", func(ctx SpecContext) {
			db, err := GetDB()
			Ω(err).Should(Succeed())

			query := "SELECT distinct(table_name) FROM information_schema.tables"
			rows, err := db.Query(query)
			Ω(err).Should(Succeed())

			expectedTables := []string{"hourly_usage", "mcp"}
			for rows.Next() {
				var tableName string
				err = rows.Scan(&tableName)
				Ω(err).Should(Succeed())

				Ω(len(expectedTables)).ShouldNot(Equal(0))
				expectedTables = slices.DeleteFunc(expectedTables, func(expected string) bool {
					return expected == tableName
				})
			}

			err = db.Close()
			Ω(err).Should(Succeed())
		})

	})

	var resourceEntry HourlyUsageEntry
	var expectedResourceName = "test-project-test-workspace-test-mcp"
	Context("Database Entities", Ordered, func() {
		BeforeAll(func() {
			resourceEntry = HourlyUsageEntry{
				Project:   "test-project",
				Workspace: "test-workspace",
				Name:      "test-mcp",
			}
		})
		It("should return the correct ResourceName", func() {
			Ω(resourceEntry.ResourceName()).Should(Equal(expectedResourceName))
		})
		It("should return the correct ObjectKey", func() {
			Ω(resourceEntry.ObjectKey()).Should(Equal(k8s.ObjectKey{
				Name: expectedResourceName,
			}))
		})
	})
})
