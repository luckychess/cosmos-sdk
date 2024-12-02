package testutil

import (
	"context"

	bankkeeper "cosmossdk.io/x/bank/v2/keeper"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// FundAccount is a utility function that funds an account by minting and
// sending the coins to the address. This should be used for testing purposes
// only!
func FundAccount(ctx context.Context, bankKeeper bankkeeper.Keeper, addr []byte, amount sdk.Coin) error {
	return bankKeeper.MintCoin(ctx, addr, amount)
}
