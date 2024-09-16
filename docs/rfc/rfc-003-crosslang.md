# RFC 003: Cross Language Account/Module Execution Model

## Changelog

* 2024-08-09: Reworked initial draft (previous work was in https://github.com/cosmos/cosmos-sdk/pull/15410)
* 2024-09-04: Added message packet, error handling and gas specifications

## Background

The Cosmos SDK has historically been a Golang only framework for building blockchain applications.
However, discussions about supporting additional programming languages and virtual machine environments
have been underway since early 2023. Recently, we have identified the following key target user groups:
Recently we have identified the following key target user groups:
1. projects that want to primarily target a single programming language and virtual machine environment besides Golang but who still want to use Cosmos SDK internals for consensus and storage
2. projects that want to integrate multiple programming languages and virtual machine environments into an integrated application

While these two user groups may have substantially different needs,
the goals of the second group are more challenging to support and require a more clearly specified unified design.

This RFC primarily attempts to address the needs of the second group.
However, in doing so, it also intends to address the needs of the first group as we will likely build many of the components needed for this group by building an integrated cross-language, cross-VM framework.
Those needs of the first group which are not satisfactorily addressed by the cross-language framework should be addressed in separate RFCs.

Prior work on cross-language support in the SDK includes:
- [RFC 003: Language-independent Module Semantics & ABI](https://github.com/cosmos/cosmos-sdk/pull/15410): an earlier, significantly different and unmerged version of this RFC.
- [RFC 002: Zero Copy Encoding](./rfc-002-zero-copy-encoding.md): a zero-copy encoding specification for ProtoBuf messages, which was partly implemented and may or may not still be relevant to the current work.

Also, this design largely builds on the existing `x/accounts` module and extends that paradigm to environments beyond just Golang.
That design was specified in [RFC 004: Accounts](./rfc-004-accounts.md).

## Proposal

We propose a conceptual and formal model for defining **accounts** and **modules** which can interoperate with each other through **messages** in a cross-language, cross-VM environment.

We start with the conceptual definition of core concepts from the perspective of a developer
trying to write code for a module or account. 
The formal details of how these concepts are represented in a specific coding environment may vary significantly,
however, the essence should remain more or less the same in most coding environments. 

This specification is intentionally kept minimal as it is always easier to add features than to remove them.
Where possible, other layers of the system should be specified in a complementary, modular way in separate specifications.

### Account

An **account** is defined as having:
* a unique **address**
* an **account handler** which is some code which can process **messages** and send **messages** to other **accounts**

### Address

An **address** is defined as a variable-length byte array of up to 63 bytes
so that an address can be represented as a 64-byte array with the first byte indicating the length of the address.

### Message

A **message** is defined as a tuple of:
* a **message name** 
* and **message data**

A **message name** is an ASCII string of up to 127 characters
so that it can be represented as a 128-byte array with the first byte indicating the length of the string. 
**Message names** can only contain letters, numbers and the special characters `:`, `_`, `/`, and `.`.

**Message data** will be defined in more detail later.

### Account Handler

The code that implements an account's message handling is known as the **account handler**. The handler receives a **message request** and can return some **message response** or an error.

The handler for a specific message within an **account handler** is known as a **message handler**.

### Message Request

A **message request** contains:
* the **address** of the **account** (its own address)
* the **address** of the account sending the message (the **caller**), which will be empty if the message is a query
* the **message name**
* the **message data**
* a 32-byte **state token**
* a 32-byte **context token**
* a `uint64` **gas limit**

**Message requests** can also be prepared by **account handlers** to send **messages** to other accounts.

### Modules and Modules Messages

There is a special class of **message**s known as **module messages**,
where the caller should omit the address of the receiving account.
The routing framework can look up the address of the receiving account based on the message name of a **module message**.

Accounts which define handlers for **module messages** are known as **modules**.

**Module messages** are distinguished from other messages because their message name must start with the `module:` prefix.

The special kind of account handler which handles **module messages** is known as a **module handler**.
A **module** is thus an instance of a **module handler** with a specific address 
in the same way that an account is an instance of an account handler.
In addition to an address, **modules** also have a human-readable **module name**.

More details on **modules** and **module messages** will be given later.

### Account Handler and Message Metadata

Every **account handler** is expected to provide metadata which provides:
* a list of the **message names** it defines **message handlers** and for each of these, its:
  * **volatility** (described below)
  * optional additional bytes, which are not standardized at this level
* **state config** bytes which are sent to the **state handler** (described below) but are otherwise opaque
* some optional additional bytes, which are not standardized at this level

### Account Lifecycle

**Accounts** can be created, destroyed and migrated to new **account handlers**.

**Account handlers** can define message handlers for the following special message name's:
* `on_create`: called when an account is created with message data containing arbitrary initialization data.
* `on_migrate`: called when an account is migrated to a new code handler. Such handlers receive structured message data specifying the old code handler so that the account can perform migration operations or return an error if migration is not possible.

### Hypervisor and Virtual Machines

Formally, a coding environment where **account handlers** are run is known as a **virtual machine**.
These **virtual machine**s may or may not be sandboxed virtual machines in the traditional sense.
For instance, the existing Golang SDK module environment (currently specified by `cosmossdk.io/core`), will
be known as the "native Golang" virtual machine.
For consistency, however,
we refer to these all as **virtual machines** because from the perspective of the cross-language framework,
they must implement the same interface.

The special module which manages **virtual machines** and **accounts** is known as the **hypervisor**.

Each **virtual machine** that is loaded by the **hypervisor** will get a unique **machine id** string.
Each **account handler** that a **virtual machine** can load is referenced by a unique **handler id** string.

There are two forms of **handler ids**:
* **module handlers** which take the form `module:<module_config_name>`
* **account handlers** which take the form `<machine_id>:<machine_handler_id>`, where `machine_handler_id` is a unique string scoped to the **virtual machine**

Each **virtual machine** must expose a list of all the **module handlers** it can run,
and the **hypervisor** will ensure that the **module handlers** are unique across all **virtual machines**.

Each **virtual machine** is expected to expose a method which takes a **handler id**
and returns a reference to an **account handler**
which can be used to run **messages**.
**Virtual machines** will also receive an `invoke` function
so that their **account handlers** can send messages to other **accounts**.
**Virtual machines** must also implement a method to return the metadata for each **account handler** by **handler id**.

### State and Volatility

Accounts generally also have some mutable state, but within this specification,
state is mostly expected to be handled by some special state module defined by separate specifications.
The few main concepts of **state handler**, **state token**, **state config** and **volatility** are defined here.

The **state handler** is a system component which the hypervisor has a reference to,
and which is responsible for managing the state of all accounts.
It only exposes the following methods to the hypervisor:
- `create(account address, state config)`: creates a new account state with the specified address and **state config**.
- `migrate(account address, new state config)`: migrates the account state with the specified address to a new state config
- `destroy(account address)`: destroys the account state with the specified address
- `begin_tx(state_token, account address)`: creates a nested transaction within the specified state token for the specified account address
- `commit_tx(state token)`: commits any state changes in the current nested transaction of the specified state token
- `rollback_tx(state token)`: discards any state changes in the current nested transaction of the specified state token
- `discard_cleanup(state token)`: cleans up and discards the current state token

It is expected that _only_ the hypervisor can call the above methods.

**State config** are optional bytes that each account handler's metadata can define which get passed to the **state handler** when an account is created.
These bytes can be used by the **state handler** to determine what type of state and commitment store the **account** needs.

`begin_tx`, `commit_tx`, `rollback_tx` and `discard_cleanup` are used internally by the hypervisor for error handling. See the error handling section for more details on their usage.

A **state token** is an array of 32-bytes that is passed in each message request.
It is opaque to the hypervisor except that the first bit of the first byte (the high bit)
indicates the volatility of the state token.
If the high bit is set then the state token is **volatile**, and if it is unset, it is **readonly**.
Otherwise, the hypervisor has no knowledge of what this token represents or how it is created.
It is expected that modules that manage state do understand this token and use it to manage all state changes
in consistent transactions.
All side effects regarding state, events, etc. are expected to coordinate around the usage of this token.
It is possible that state modules expose methods for creating new **state tokens**
for nesting transactions.

**Volatility** describes a message handler's behavior with respect to state and side effects.
It is an enum value that can have one of the following values:
* `volatile`: the handler can have side effects and send `volatile`, `readonly` or `pure` messages to other accounts. Such handlers are expected to both read and write state.
* `readonly`: the handler cannot cause effects side effects and can only send `readonly` or `pure` messages to other accounts. Such handlers are expected to only read state.
* `pure`: the handler cannot cause any side effects and can only call other pure handlers. Such handlers are expected to neither read nor write state.

The hypervisor will enforce **volatility** rules when routing messages to account handlers.
Caller addresses are always passed to `volatile` methods,
they are not required when calling `readonly` methods but will be passed when available,
and they are not passed at all to `pure` methods.
Volatile handlers can only be called with volatile state tokens.
Readonly handlers cannot call volatile handlers even if they receive a volatile state token,
except in the case that they acquire a new volatile state token.
This can be used for simulations.

### Management of Account Lifecycle with the Hypervisor

In order to manage **accounts** and their mapping to **account handlers**, the **hypervisor** contains stateful mappings for:
* **account address** to **handler id**
* **module name** to module **account address** and **module config**
* **message name** to **account address** for **module messages**

The **hypervisor** as a first-class module itself handles the following special **module messages** to manage account
creation, destruction, and migration:
* `create(handler_id, init_data) -> address`: creates a new account in the specified code environment with the specified handler id and returns the address of the new account. The `on_create` message is called if it is implemented by the  account. Addresses are generated deterministically by the hypervisor with a configurable algorithm which will allow public key accounts to get predictable addresses.
* `destroy(address)`: deletes the account with the specified address. `destroy` can only be called by the account itself.
* `migrate(address, new_handler_id)`: migrates the account with the specified address to the new account handler. The `on_migrate` message must be implemented by the new code and must not return an error for migration to succeed. `migrate` can only be called by the account itself.
* `force_migrate(address, new_handler_id, init_data)`: this can be used when no `on_migrate` handler can perform a proper migration to the new account handler. In this case, the old account state will be destroyed, and `on_create` will be called on the new code. This is a destructive operation and should be used with caution.

The **hypervisor** will call the **state handler**'s `create`, `migrate`,
and `destroy` methods as needed when accounts are created, migrated, or destroyed.

### Module Lifecycle & Module Messages

For legacy purposes, **modules** have specific lifecycles and **module messages** have special semantics.
A **module handler** cannot be loaded with the `create` message,
but must be loaded by an external call to the hypervisor
which includes the **module name** and **module config** bytes.
The existing `cosmos.app.v1alpha1.Config` can be used for this purpose if desired.

**Module messages** also allow the definition of pre- and post-handlers.
These are special message handlers that can only be defined in **module handlers**
and must be prefixed by the `module:pre:` or `module:post:` prefixes
When modules are loaded in the hypervisor, a composite message handler will be composed using all the defined
pre- and post-handlers for a given message name in the loaded module set.
By default, the ordering will be done alphabetically by module name.

### Authorization and Delegated Execution

When a message handler creates a message request, it can pass any address as the caller address,
but it must pass the same **context token** that it received in its message request.
The hypervisor will use the **context token** to verify the "real" caller address.
Every nested message call will receive a new non-forgeable **context token** so that virtual machines
and their account handlers cannot arbitrarily fool the hypervisor about the real caller address.

By default, the hypervisor will only allow the real caller to act as the caller.

There are use cases, however, for delegated authorization of messages or even for modules which can execute
a message on behalf of any account.
To support these, the hypervisor will accept an **authorization middleware** parameter which checks
whether a given real caller account (verified by the hypervisor) is authorized to act as a different caller
account for a given message request.

### Message Data and Packet Specification

To facilitate efficient cross-language and cross-VM message passing, the precise layout of **message packets** is important
as it reduces the need for serialization and deserialization in the core hypervisor and virtual machine layers.

Message packets have a minimum size of 512 bytes to accommodate the 512-byte header specified below.
Larger packets may be allocated depending on message handler needs.
Generally, each message handler specification should describe the precise utilization of the message packet
for that handler.

#### Message Packet Header

**Message packets** always start with the 512-byte header with the following layout.
- **message name**: a 128-byte array with the first byte indicating the length of the message name
- **self-address**: a 64-byte array with the first byte indicating the length of the address
- **caller-address**: a 64-byte array with the first byte indicating the length of the address
- **context token**: a 32-byte array
- **state token**: a 32-byte array
- **message name hash**: the first 8-bytes of the SHA256 hash of the message name, which can be used for simplified routing
- **gas limit**: an unsigned 64-bit integer
- **gas consumed**: an unsigned 64-bit integer
- **input data pointer 1**: 16 bytes, see below for the **data pointer** spec
- **input data pointer 2**: 16 bytes
- **output data pointer 1**: 16 bytes
- **output data pointer 2**: 16 bytes
- remaining bytes: reserved for future use, should be zeroed when a packet is initialized

#### Data Pointer

A **data pointer** is specified as follows:
- **native pointer**: 8-bytes, a pointer to a separate buffer in the native environment or zero
- **length**: unsigned 32-bit integer
- **capacity or offset**: unsigned 32-bit integer

In a **data pointer**, if **native pointer** is zero, then **capacity or offset** points to an offset within the
**message packet** to the start of the data.
Any such offset must occur after the 512-byte **message packet** header.
If **native pointer** is non-zero, then **capacity or offset** is the capacity of the allocated buffer in bytes,
which should be used when freeing the buffer.
The **length** value indicates the length of the data in bytes that needs to be copied from source to target
when data is passed from one environment to another.

When passing a packet from one environment to another, a VM should follow these steps:
1. allocate the packet in the new environment
2. copy all 512-bytes of the **message packet** header from the source packet to the target packet
3. if input data pointers point to additional buffers, then allocate these buffers in the new environment
and update the **data pointers** in the target packet to point to the new buffers
4. (optional) output data pointers may point to pre-allocated buffers or regions, but should generally have zero-length, if needed, do special handling of these
5. after execution, copy and allocate output buffers as needed,
and update the **output data pointers** in the source packet to point to the appropriate memory regions

### Error Codes & Handling

All invoke and handler methods return a 32-bit unsigned integer error code.
If the error code is zero, then the operation was successful.

#### System-Level Error Codes
The following non-zero error codes are defined at the system level:
* 1: out of gas
* 2: fatal execution error (when an unexpected, likely non-deterministic fatal error occurs)
* 3: account not found
* 4: message handler not found
* 5: invalid state access (when volatility rules are violated)
* 6: unauthorized caller address (when the caller address impersonation is not authorized)
* 7: invalid handler (when hypervisor or vm-level rules are violated by the handler implementation)
* 8: unknown handler error (when handler execution failed for an unknown, but likely deterministic reason)

Error codes through 255 are reserved for system-level errors and should only be returned by the hypervisor or 
a virtual machine implementation.

#### Handler-level Error Codes

Any error code above 255 is interpreted as an error returned by a handler itself and is to be interpreted by the caller
based on the message handler's specification.

It is an error for a handler implementation to return a system-level error code, and if such a code is received it
will be translated to error code 7 (invalid handler).
If a handler implementation wants to simply propagate a system-level error code that it receives,
it should wrap it with a different error code.
Developer SDKs should handle this wrapping automatically.

#### Error Data

If additional data is included in the error,
it is generally expected that this would be referenced by **output data pointer 1**,
although message handlers are free to use **output data pointer 2** if necessary as well.
System-level errors, if they do include additional data, will encode it as a string in **output data pointer 1**
and will limit such messages to a maximum of 255 bytes.

#### State Transactions and Errors

Whenever a volatile message handler returns an error, no side effects can occur.
If any side effects were applied to state before the error occurred, these must be rolled back.
The hypervisor manages this using the `begin_tx`, `commit_tx`, and `rollback_tx` on the **state handler**.

Before any call to a method,
the hypervisor will call `begin_tx` with the state token passed in the message request
and the authenticated caller address tracked by the hypervisor.
If the method returns the core `0` for success, then the hypervisor will call `commit_tx` with the state token.
If the method returns an error code, then the hypervisor will call `rollback_tx` with the state token
and return the error to the caller.
The state handler should use the account address passed in `begin_tx` to identify the state
location that is being modified, rather than the caller address because the
caller address can be impersonated with authorization middleware.

This ensures state consistency in the presence of errors.

#### Unwinding Errors and Discarding State Tokens

Error codes 1 (out of gas) and 2 (fatal execution error) are considered unwinding errors.
If one of these errors is returned, the hypervisor will halt execution of the calling message handler
and unwind the call stack up to the last recoverable message handler if there is one.
When unwinding, the hypervisor will call the `discard_cleanup` method of the **state handler**
with each _new_ state token that was introduced in the unwound call stack.
This ensures that if message handlers used new state tokens to create nested transactions,
these transactions are properly cleaned up.
Any state token that was used in the call stack before the unwinding target will not be cleaned up.

For out-of-gas errors (code 1), the hypervisor will unwind up to the caller that set the gas limit.
For fatal execution errors (code 2), the hypervisor will unwind up to the external caller that called the hypervisor, likely
resulting in process termination.
A fatal execution error should only be returned when it is expected that the error is non-deterministic and unrecoverable.
An example of such an error would be running out of disk space or losing network connectivity.
If there is an execution error that is likely to be deterministic, such as Wasm code failing to execute, then
the virtual machine should return error code 8 (unknown handler error), which is not an unwinding error.

### Gas

Gas is a measure of computational resources consumed by a message handler.
Whenever a gas limit is imposed, if at any point that gas limit is exceeded,
execution will halt and an out-of-gas error will be returned to the last handler
executing without a gas limit.
If at the execution root (made via a call external to the hypervisor), the gas limit is
set to zero, then execution of that handler is unmetered and essentially infinite.
Such unmetered handlers may set any gas limit they wish for nested calls.
Once there is a gas limit, gas consumed is a monotonically increasing value that can't be bypassed
and is unaffected by any nesting of state tokens.
When making calls when there is a gas limit set, a caller can either choose to inherit the existing
gas limit or set a more restrictive gas limit.
Any gas that a handler marks as consumed in a message packet should be added to any gas that a
virtual machine has metered for that handler when returning consumed gas to the hypervisor.

## Abandoned Ideas (Optional)

## Decision

Based on internal discussions, we have decided to move forward with this design. 

## Consequences (optional)

### Backwards Compatibility

It is intended that existing SDK modules built using `cosmossdk.io/core` and
account handlers built with `cosmossdk.io/x/accounts` can be integrated into this system with zero or minimal changes.

### Positive

This design will allow native SDK modules to be built using other languages such as Rust and Zig, and
for modules to be executed in different virtual machine environments such as Wasm and the EVM.
It also extends the concept of a module to first-class accounts in the style of the existing `x/accounts` module
and EVM contracts.

### Negative

### Neutral

Similar to other message passing designs,
the raw performance invoking a message handler will be slower than a golang method call as in the existing keeper paradigm.

However, this design does nothing to preclude the continued existence of golang native keeper passing, and it is likely
that we can find performance optimizations in other areas to mitigate any performance loss.
In addition, a cross-language, cross-VM is simply not possible without some overhead.


### References

- [Abandoned RFC 003: Language-independent Module Semantics & ABI](https://github.com/cosmos/cosmos-sdk/pull/15410)
- [RFC 002: Zero Copy Encoding](./rfc-002-zero-copy-encoding.md) 
- [RFC 004: Accounts](./rfc-004-accounts.md)

## Discussion

This specification does not cover many important parts of a complete system such as the encoding of message data,
storage, events, transaction execution, or interaction with consensus environments.
It is the intention of this specification to specify the minimum necessary for this layer in a modular layer.
The full framework should be composed of a set of independent, minimally defined layers that together
form a "standard" execution environment, but that at the same time can be replaced and recomposed by
different applications with different needs.

The basic set of standards necessary to provide a coherent framework includes:
* message encoding and naming, including compatibility with the existing protobuf-based message encoding
* storage
* events
* authorization middleware