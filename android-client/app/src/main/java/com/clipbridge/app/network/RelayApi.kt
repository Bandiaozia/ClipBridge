package com.clipbridge.app.network

import com.clipbridge.app.crypto.Identity
import com.clipbridge.app.domain.model.Device
import com.clipbridge.app.domain.model.Tokens
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONArray
import org.json.JSONObject
import java.io.IOException
import java.util.concurrent.TimeUnit

class RelayApi(
    private val client: OkHttpClient,
    private val serverUrl: () -> String,
) {
    suspend fun authenticate(
        email: String,
        password: String,
        identity: Identity,
        register: Boolean,
    ): Tokens = withContext(Dispatchers.IO) {
        val device = JSONObject()
            .put("id", identity.deviceId)
            .put("name", identity.name)
            .put("platform", "android")
            .put("x25519_public_key", identity.xPublicBase64())
            .put("ed25519_public_key", identity.signPublicBase64())
        val body = JSONObject()
            .put("email", email.trim())
            .put("password", password)
            .put("device", device)
        val path = if (register) "/api/v1/auth/register" else "/api/v1/auth/login"
        val response = execute("POST", path, body)
        parseTokens(response.getJSONObject("tokens"))
    }

    suspend fun refresh(refreshToken: String): Tokens = withContext(Dispatchers.IO) {
        parseTokens(
            execute(
                "POST",
                "/api/v1/auth/refresh",
                JSONObject().put("refresh_token", refreshToken),
            ),
        )
    }

    suspend fun devices(accessToken: String): List<Device> = withContext(Dispatchers.IO) {
        val array = execute("GET", "/api/v1/devices", token = accessToken)
            .getJSONArray("devices")
        buildList {
            for (index in 0 until array.length()) add(parseDevice(array.getJSONObject(index)))
        }
    }

    suspend fun logout(accessToken: String, refreshToken: String) =
        withContext(Dispatchers.IO) {
            execute(
                "POST",
                "/api/v1/auth/logout",
                JSONObject().put("refresh_token", refreshToken),
                accessToken,
                allowEmpty = true,
            )
        }

    suspend fun createPairing(accessToken: String): JSONObject =
        withContext(Dispatchers.IO) {
            execute("POST", "/api/v1/pairing/create", JSONObject(), accessToken)
        }

    suspend fun acceptPairing(accessToken: String, token: String): JSONObject =
        withContext(Dispatchers.IO) {
            execute(
                "POST",
                "/api/v1/pairing/accept",
                JSONObject().put("token", token),
                accessToken,
            )
        }

    private fun execute(
        method: String,
        path: String,
        body: JSONObject? = null,
        token: String? = null,
        allowEmpty: Boolean = false,
    ): JSONObject {
        val builder = Request.Builder()
            .url(serverUrl().trimEnd('/') + path)
            .header("Accept", "application/json")
            .header("User-Agent", "ClipBridge-Android/0.4")
        token?.let { builder.header("Authorization", "Bearer $it") }
        val requestBody = body?.toString()
            ?.toRequestBody("application/json; charset=utf-8".toMediaType())
        when (method) {
            "POST" -> builder.post(requestBody ?: ByteArray(0).toRequestBody())
            "DELETE" -> builder.delete(requestBody)
            else -> builder.get()
        }
        client.newCall(builder.build()).execute().use { response ->
            val text = response.body?.string().orEmpty()
            if (!response.isSuccessful) {
                val message = runCatching {
                    JSONObject(text).getJSONObject("error").getString("message")
                }.getOrDefault("服务器请求失败（${response.code}）")
                throw IOException(message)
            }
            if (allowEmpty && text.isBlank()) return JSONObject()
            return JSONObject(text)
        }
    }

    private fun parseTokens(json: JSONObject) = Tokens(
        json.getString("access_token"),
        json.getString("refresh_token"),
        System.currentTimeMillis() + json.getLong("expires_in") * 1000,
    )

    private fun parseDevice(json: JSONObject) = Device(
        json.getString("id"),
        json.getString("name"),
        json.getString("platform"),
        json.getString("x25519_public_key"),
        json.getString("ed25519_public_key"),
        json.optBoolean("online"),
        json.has("revoked_at"),
    )

    companion object {
        fun defaultClient(): OkHttpClient = OkHttpClient.Builder()
            .connectTimeout(10, TimeUnit.SECONDS)
            .readTimeout(15, TimeUnit.SECONDS)
            .writeTimeout(15, TimeUnit.SECONDS)
            .callTimeout(20, TimeUnit.SECONDS)
            .pingInterval(25, TimeUnit.SECONDS)
            .build()
    }
}

