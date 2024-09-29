//! Rust Cosmos SDK RFC 003 hypervisor implementation.
use std::alloc::Layout;
use std::cell::RefCell;
use std::collections::HashMap;
use std::ops::DerefMut;
use std::sync::Arc;
use ixc_message_api::AccountID;
use ixc_message_api::code::{ErrorCode, SystemErrorCode};
use ixc_message_api::handler::{AllocError, RawHandler, HostBackend, HandlerErrorCode};
use ixc_message_api::packet::MessagePacket;
use ixc_vm_api::{HandlerID, VM};
use ixc_core_macros::message_selector;
use ixc_message_api::header::MessageHeader;

/// Rust Cosmos SDK RFC 003 hypervisor implementation.
pub struct Hypervisor<ST: StateHandler> {
    vmdata: Arc<VMData>,
    state_handler: ST,
}

struct VMData {
    vms: HashMap<String, Box<dyn VM>>,
}

impl<ST: StateHandler> Hypervisor<ST> {
    /// Create a new hypervisor with the given state handler.
    pub fn new(state_handler: ST) -> Self {
        Self {
            vmdata: Arc::from(VMData {
                vms: HashMap::new(),
            }),
            state_handler,
        }
    }

    /// Register a VM with the hypervisor.
    pub fn register_vm(&mut self, name: &str, vm: Box<dyn VM>) -> Result<(), ()> {
        let mut vmdata = Arc::get_mut(&mut self.vmdata).ok_or(())?;
        vmdata.vms.insert(name.to_string(), vm);
        Ok(())
    }

    /// Invoke a message packet.
    pub fn invoke(&self, message_packet: &mut MessagePacket) -> Result<(), ErrorCode> {
        let mut tx = self.state_handler.new_transaction();
        tx.push_frame(message_packet.header().sender_account, true).map_err(
            |e| match e {
                PushFrameError::VolatileAccessError => ErrorCode::RuntimeSystemError(SystemErrorCode::InvalidHandler),
            }
        )?;
        let mut exec_context = ExecContext {
            vmdata: self.vmdata.clone(),
            tx: RefCell::new(tx),
        };
        let res = exec_context.invoke(message_packet);
        let tx = exec_context.tx.into_inner();
        if res.is_ok() {
            self.state_handler.commit(tx);
        } else {
            tx.rollback();
        }

        res
    }
}

/// The state handler traits the hypervisor expects.
pub trait StateHandler {
    /// The transaction type.
    type Tx: Transaction;
    /// Create a new transaction.
    fn new_transaction(&self) -> Self::Tx;
    /// Commit a transaction.
    fn commit(&self, tx: Self::Tx);
}

/// A transaction.
pub trait Transaction {
    /// The key-value store type.
    type KVStore: KVStore;

    /// Initialize the account storage.
    fn init_account_storage(&mut self, account: AccountID, storage_params: &[u8]);
    /// Push a new execution frame.
    fn push_frame(&mut self, account: AccountID, volatile: bool) -> Result<(), PushFrameError>;
    /// Pop the current execution frame.
    fn pop_frame(&mut self, commit: bool) -> Result<(), PopFrameError>;
    /// Get the active account.
    fn active_account(&self) -> AccountID;
    /// Rollback the transaction.
    fn rollback(self);
    /// A mutable kv-store instance for the hypervisor to manage its own state.
    fn manager_state(&self) -> &mut Self::KVStore;
}

/// A push frame error.
#[non_exhaustive]
pub enum PushFrameError {
    /// Tried to push a volatile frame on top of a non-volatile frame.
    VolatileAccessError,
}

/// A pop frame error.
#[non_exhaustive]
pub enum PopFrameError {
    /// No frames to pop.
    NoFrames,
}

/// A key-value store.
pub trait KVStore {
    /// Get a value.
    fn get(&self, key: &[u8]) -> Option<Vec<u8>>;
    /// Set a value.
    fn set(&mut self, key: &[u8], value: &[u8]);
    /// Delete a value.
    fn delete(&mut self, key: &[u8]);
}

struct ExecContext<TX: Transaction> {
    vmdata: Arc<VMData>,
    tx: RefCell<TX>,
}

impl<'a, TX: Transaction> ExecContext<TX> {
    fn get_account_handler_id(&self, tx: &mut TX, account_id: AccountID) -> Option<HandlerID> {
        let kv_store = tx.manager_state();
        let key = format!("h:{}", account_id.get());
        let value = kv_store.get(key.as_bytes())?;
        parse_handler_id(&value)
    }

    fn next_account_id(&self, tx: &mut TX) -> AccountID {
        let kv_store = tx.manager_state();
        let id = kv_store.get(b"next_account_id").map_or(ACCOUNT_ID_NON_RESERVED_START, |v| {
            u64::from_le_bytes(v.try_into().unwrap())
        });
        kv_store.set(b"next_account_id", &(id + 1).to_le_bytes());
        AccountID::new(id)
    }
}

const ACCOUNT_ID_NON_RESERVED_START: u64 = u16::MAX as u64 + 1;

fn parse_handler_id(value: &[u8]) -> Option<HandlerID> {
    let mut parts = value.split(|&c| c == b':');
    let vm = parts.next()?;
    let handler_id = parts.next()?;
    Some(HandlerID {
        vm: String::from_utf8(vm.to_vec()).ok()?,
        vm_handler_id: String::from_utf8(handler_id.to_vec()).ok()?,
    })
}

impl<TX: Transaction> HostBackend for ExecContext<TX> {
    fn invoke(&self, message_packet: &mut MessagePacket) -> Result<(), ErrorCode> {
        // get the mutable transaction from the RefCell
        let mut tx = self.tx.try_borrow_mut()
            .map_err(|_| ErrorCode::RuntimeSystemError(SystemErrorCode::FatalExecutionError))?;
        let mut tx = tx.deref_mut();

        // check if the caller matches the active account
        let account = tx.active_account();
        if message_packet.header().sender_account != account {
            return Err(ErrorCode::RuntimeSystemError(SystemErrorCode::UnauthorizedCallerAccess));
        }
        // TODO support authorization middleware

        let target_account = message_packet.header().account;
        // check if the target account is a system account
        if target_account.is_null() {
            return self.handle_system_message(&mut tx, message_packet);
        }

        // find the account's handler ID and retrieve its VM
        let handler_id = self.get_account_handler_id(&mut tx, target_account).
            ok_or(ErrorCode::RuntimeSystemError(SystemErrorCode::HandlerNotFound))?;
        let vm = self.vmdata.vms.get(&handler_id.vm).
            ok_or(ErrorCode::RuntimeSystemError(SystemErrorCode::HandlerNotFound))?;

        // push an execution frame for the target account
        tx.push_frame(target_account, false). // TODO add volatility support
            map_err(|_| ErrorCode::RuntimeSystemError(SystemErrorCode::InvalidHandler))?;
        // run the handler
        let res = vm.run_handler(&handler_id.vm_handler_id, message_packet, self);
        // pop the execution frame
        tx.pop_frame(res.is_ok()).
            map_err(|_| ErrorCode::RuntimeSystemError(SystemErrorCode::InvalidHandler))?;

        res
    }

    unsafe fn alloc(&self, layout: Layout) -> Result<*mut u8, AllocError> {
        Ok(std::alloc::alloc(layout))
    }
}

impl<TX: Transaction> ExecContext<TX> {
    fn handle_system_message(&self, tx: &mut TX, message_packet: &mut MessagePacket) -> Result<(), ErrorCode> {
        match message_packet.header().message_selector {
            CREATE_SELECTOR => unsafe {
                // get the input data
                let handler_id = message_packet.header().in_pointer1.get(message_packet);
                let init_data = message_packet.header().in_pointer2.get(message_packet);

                // resolve the handler ID and retrieve the VM
                let handler_id = parse_handler_id(handler_id).
                    ok_or(ErrorCode::RuntimeSystemError(SystemErrorCode::HandlerNotFound))?;
                let vm = self.vmdata.vms.get(&handler_id.vm).
                    ok_or(ErrorCode::RuntimeSystemError(SystemErrorCode::HandlerNotFound))?;
                let desc = vm.describe_handler(&handler_id.vm_handler_id).
                    ok_or(ErrorCode::RuntimeSystemError(SystemErrorCode::HandlerNotFound))?;

                // get the next account ID and initialize the account storage
                let id = self.next_account_id(tx);
                let storage_params = desc.storage_params.unwrap_or_default();
                tx.init_account_storage(id, &storage_params);

                // create a packet for calling on_create
                let mut on_create_header = MessageHeader::default();
                // TODO: how do we specify a selector that can only be called by the system?
                on_create_header.message_selector = ON_CREATE_SELECTOR;
                on_create_header.in_pointer1.set_slice(init_data);
                let on_create_header_ptr: *mut MessageHeader = &mut on_create_header;
                let mut on_create_packet = unsafe { MessagePacket::new(on_create_header_ptr, size_of::<MessageHeader>()) };

                // push a volatile frame and execute the on_create handler
                tx.push_frame(id, true).
                    map_err(|_| ErrorCode::RuntimeSystemError(SystemErrorCode::InvalidHandler))?;
                let res = vm.run_handler(&handler_id.vm_handler_id, &mut on_create_packet, self);
                tx.pop_frame(res.is_ok()).
                    map_err(|_| ErrorCode::RuntimeSystemError(SystemErrorCode::FatalExecutionError))?;

                match res {
                    Err(ErrorCode::HandlerSystemError(HandlerErrorCode::MessageNotHandled)) => {
                        // the on_create handler was not found which is not an error,
                        // so we return success
                        Ok(())
                    }
                    _ => res
                }
            },
            _ => {
                Err(ErrorCode::RuntimeSystemError(SystemErrorCode::HandlerNotFound))
            }
        }
    }
}

const CREATE_SELECTOR: u64 = message_selector!("ixc.account.v1.create");
const ON_CREATE_SELECTOR: u64 = message_selector!("ixc.account.v1.on_create");


#[cfg(test)]
mod tests {
    #[test]
    fn test_parse_handler_id() {
        let value = b"vm1:handler1";
        let handler_id = super::parse_handler_id(value).unwrap();
        assert_eq!(handler_id.vm, "vm1");
        assert_eq!(handler_id.vm_handler_id, "handler1");
    }
}