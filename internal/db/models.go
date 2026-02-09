package db

import "github.com/gitshopapp/gitshop/internal/models"

type Shop = models.Shop
type Order = models.Order
type OrderStatus = models.OrderStatus

const (
	StatusPendingPayment = models.StatusPendingPayment
	StatusPaid           = models.StatusPaid
	StatusPaymentFailed  = models.StatusPaymentFailed
	StatusExpired        = models.StatusExpired
	StatusShipped        = models.StatusShipped
	StatusDelivered      = models.StatusDelivered
	StatusRefunded       = models.StatusRefunded
)
