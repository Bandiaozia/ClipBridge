package com.clipbridge.app.presentation

import androidx.lifecycle.ViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow

data class AuthFormState(
    val email: String = "",
    val password: String = "",
    val register: Boolean = false,
)

class AuthFormViewModel : ViewModel() {
    private val _state = MutableStateFlow(AuthFormState())
    val state = _state.asStateFlow()

    fun updateEmail(value: String) {
        _state.value = _state.value.copy(email = value.take(254))
    }

    fun updatePassword(value: String) {
        _state.value = _state.value.copy(password = value.take(256))
    }

    fun toggleMode() {
        _state.value = _state.value.copy(register = !_state.value.register)
    }

    fun validationError(): String? = when {
        !_state.value.email.contains('@') -> "请输入有效邮箱"
        _state.value.password.length < 10 -> "密码至少需要 10 位"
        else -> null
    }
}

