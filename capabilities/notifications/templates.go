package notifications

import "fmt"

// renderWelcome builds the recipient/subject/body for a new-account welcome
// message.
func renderWelcome(e UserCreatedEvent) (to, subject, body string) {
	name := e.Name
	if name == "" {
		name = e.Email
	}
	return e.Email, "Welcome!", fmt.Sprintf("Hi %s, your account has been created.", name)
}

// renderPaymentReceipt builds the recipient/subject/body for a completed-
// payment receipt.
func renderPaymentReceipt(e PaymentCompletedEvent) (to, subject, body string) {
	return e.Email, "Payment received", fmt.Sprintf("We received your payment of %s for order %s.", e.Amount, e.OrderID)
}

// renderMembershipStarted builds the recipient/subject/body for a new-
// membership welcome message.
func renderMembershipStarted(e MembershipEvent) (to, subject, body string) {
	return e.Email, "Welcome to " + e.PlanName, fmt.Sprintf("Your %s membership is now active.", e.PlanName)
}

// renderMembershipExpired builds the recipient/subject/body for a
// membership-expiry notice.
func renderMembershipExpired(e MembershipEvent) (to, subject, body string) {
	return e.Email, "Your membership has expired", fmt.Sprintf("Your %s membership has expired. Renew to keep your access.", e.PlanName)
}
