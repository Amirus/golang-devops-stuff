package md_test

import (
	"github.com/cloudfoundry/hm9000/models"
	"github.com/cloudfoundry/hm9000/testhelpers/appfixture"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Stopping Duplicate Instances", func() {
	var a appfixture.AppFixture

	Context("when there are multiple instances on the same index", func() {
		var instance0, instance1, duplicateInstance1 appfixture.Instance
		var heartbeat models.Heartbeat
		BeforeEach(func() {
			a = appfixture.NewAppFixture()

			instance0 = a.InstanceAtIndex(0)
			instance1 = a.InstanceAtIndex(1)
			duplicateInstance1 = a.InstanceAtIndex(1)
			duplicateInstance1.InstanceGuid = models.Guid()

			heartbeat = models.Heartbeat{
				DeaGuid:            "abc",
				InstanceHeartbeats: []models.InstanceHeartbeat{instance0.Heartbeat(), instance1.Heartbeat(), duplicateInstance1.Heartbeat()},
			}
			simulator.SetCurrentHeartbeats(heartbeat)

			simulator.SetDesiredState(a.DesiredState(2))

			simulator.Tick(simulator.TicksToAttainFreshness)
		})

		It("should not immediately stop anything", func() {
			Ω(startStopListener.Stops).Should(BeEmpty())
		})

		Context("after a grace period", func() {
			Context("if both instances are still running", func() {
				BeforeEach(func() {
					simulator.Tick(simulator.GracePeriod)
				})

				It("should stop one of them", func() {
					Ω(startStopListener.Stops).Should(HaveLen(1))
					stop := startStopListener.Stops[0]
					Ω(stop.AppGuid).Should(Equal(a.AppGuid))
					Ω(stop.AppVersion).Should(Equal(a.AppVersion))
					Ω(stop.InstanceIndex).Should(Equal(1))
					Ω(stop.IsDuplicate).Should(BeTrue())
					Ω([]string{instance1.InstanceGuid, duplicateInstance1.InstanceGuid}).Should(ContainElement(stop.InstanceGuid))
				})

				Context("after another grace period (assuming the stopped instance stops)", func() {
					BeforeEach(func() {
						instanceGuidThatShouldStop := startStopListener.Stops[0].InstanceGuid

						var remainingInstance appfixture.Instance
						if instance1.InstanceGuid == instanceGuidThatShouldStop {
							remainingInstance = duplicateInstance1
						} else {
							remainingInstance = instance1
						}

						heartbeat = models.Heartbeat{
							DeaGuid:            "abc",
							InstanceHeartbeats: []models.InstanceHeartbeat{instance0.Heartbeat(), remainingInstance.Heartbeat()},
						}
						simulator.SetCurrentHeartbeats(heartbeat)
						simulator.Tick(simulator.TicksToExpireHeartbeat)
						startStopListener.Reset()
						simulator.Tick(1)
					})

					It("should not stop the other instance", func() {
						Ω(startStopListener.Stops).Should(BeEmpty())
					})
				})
			})

			Context("if only one instance is still running", func() {
				BeforeEach(func() {
					heartbeat = models.Heartbeat{
						DeaGuid:            "abc",
						InstanceHeartbeats: []models.InstanceHeartbeat{instance0.Heartbeat(), instance1.Heartbeat()},
					}
					simulator.SetCurrentHeartbeats(heartbeat)
					simulator.Tick(simulator.TicksToExpireHeartbeat)
					startStopListener.Reset()
					simulator.Tick(simulator.GracePeriod)
				})

				It("should not stop any instances", func() {
					Ω(startStopListener.Stops).Should(BeEmpty())
				})
			})
		})
	})
})