//! Resource module.

/// An account or module handler's resources.
/// This is usually derived by the state management framework.
pub unsafe trait Resources: Sized {
    /// Initializes the resources.
    unsafe fn new(scope: &ResourceScope) -> Result<Self, InitializationError>;
}

/// The resource scope.
#[derive(Default)]
pub struct ResourceScope<'a> {
    /// The prefix of all state objects under this scope.
    pub state_scope: &'a [u8],
}

/// A resource is anything that an account or module can use to store its own
/// state or interact with other accounts and modules.
pub unsafe trait StateObjectResource: Sized {
    /// Creates a new resource.
    /// This should only be called in generated code.
    /// Do not call this function directly.
    unsafe fn new(scope: &[u8], prefix: u8) -> Result<Self, InitializationError>;
}

/// An error that occurs during resource initialization.
#[derive(Debug)]
pub enum InitializationError {
    /// An non-specific error occurred.
    Other,
}