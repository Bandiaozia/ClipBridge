package com.clipbridge.app.presentation

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class AuthFormViewModelTest {
    @Test
    fun validatesAndSwitchesRegistrationMode() {
        val viewModel = AuthFormViewModel()
        viewModel.updateEmail("invalid")
        viewModel.updatePassword("short")
        assertEquals("请输入有效邮箱", viewModel.validationError())

        viewModel.updateEmail("user@example.com")
        viewModel.updatePassword("correct-horse-battery")
        assertNull(viewModel.validationError())
        viewModel.toggleMode()
        assertEquals(true, viewModel.state.value.register)
    }

    @Test
    fun limitsUntrustedInputLength() {
        val viewModel = AuthFormViewModel()
        viewModel.updateEmail("a".repeat(300))
        viewModel.updatePassword("b".repeat(400))
        assertEquals(254, viewModel.state.value.email.length)
        assertEquals(256, viewModel.state.value.password.length)
    }
}

