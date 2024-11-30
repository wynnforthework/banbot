package dev

import (
	"context"

	"github.com/banbox/banbot/web/base"

	"github.com/banbox/banbot/orm"
	"github.com/gofiber/fiber/v2"
)

func regApiDev(api fiber.Router) {
	api.Get("/orders", getOrders)
}

func getOrders(c *fiber.Ctx) error {
	type OrderArgs struct {
		TaskID int64 `query:"task_id" validate:"required"`
	}

	var data = new(OrderArgs)
	if err := base.VerifyArg(c, data, base.ArgQuery); err != nil {
		return err
	}

	sess, conn, err2 := orm.Conn(context.Background())
	if err2 != nil {
		return err2
	}
	defer conn.Release()
	orders, err2 := sess.GetOrders(orm.GetOrdersArgs{
		TaskID: data.TaskID,
	})
	if err2 != nil {
		return err2
	}

	return c.JSON(fiber.Map{
		"data": orders,
	})
}
